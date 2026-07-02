package gormstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AutomationRegistry 使用 GORM 持久化自动化服务目录。
type AutomationRegistry struct {
	db  *gorm.DB
	now func() time.Time
}

// NewAutomationRegistry 创建持久化自动化服务注册表。
func NewAutomationRegistry(db *gorm.DB) *AutomationRegistry {
	return &AutomationRegistry{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Register 注册或刷新一个自动化服务。
func (r *AutomationRegistry) Register(ctx context.Context, service automation.Service) (automation.Service, error) {
	service.ID = strings.TrimSpace(service.ID)
	if service.ID == "" {
		return automation.Service{}, fmt.Errorf("automation service id is required")
	}
	if strings.TrimSpace(service.BaseURL) == "" {
		return automation.Service{}, fmt.Errorf("automation service baseURL is required")
	}
	service.Protocol = automation.NormalizeProtocol(service.Protocol)
	now := r.now()
	if service.RegisteredAt.IsZero() {
		service.RegisteredAt = now
	}
	service.UpdatedAt = now
	for i := range service.Tasks {
		service.Tasks[i].ServiceID = service.ID
		service.Tasks[i].Key = strings.TrimSpace(service.Tasks[i].Key)
		if service.Tasks[i].Key == "" {
			return automation.Service{}, fmt.Errorf("automation task key is required")
		}
		if service.Tasks[i].Method == "" {
			service.Tasks[i].Method = http.MethodPost
		}
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model, err := automationServiceToModel(service)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name", "base_url", "health_url", "protocol", "version", "tags_json", "updated_at", "ttl_seconds",
			}),
		}).Create(&model).Error; err != nil {
			return err
		}
		if err := tx.Where("service_id = ?", service.ID).Delete(&AutomationTaskModel{}).Error; err != nil {
			return err
		}
		for _, task := range service.Tasks {
			taskModel, err := automationTaskToModel(task)
			if err != nil {
				return err
			}
			if err := tx.Create(&taskModel).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return service, err
}

// Unregister 注销自动化服务。
func (r *AutomationRegistry) Unregister(ctx context.Context, serviceID string) error {
	serviceID = strings.TrimSpace(serviceID)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("service_id = ?", serviceID).Delete(&AutomationTaskModel{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", serviceID).Delete(&AutomationServiceModel{}).Error
	})
}

// ListServices 查询未过期的自动化服务。
func (r *AutomationRegistry) ListServices(ctx context.Context) ([]automation.Service, error) {
	var models []AutomationServiceModel
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]automation.Service, 0, len(models))
	for _, model := range models {
		service, err := r.serviceFromModel(ctx, model)
		if err != nil {
			return nil, err
		}
		if r.expired(service) {
			continue
		}
		result = append(result, service)
	}
	return result, nil
}

// ListTasks 查询全部未过期服务暴露的自动化任务。
func (r *AutomationRegistry) ListTasks(ctx context.Context) ([]automation.Task, error) {
	services, err := r.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	var result []automation.Task
	for _, service := range services {
		result = append(result, service.Tasks...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ServiceID == result[j].ServiceID {
			return result[i].Key < result[j].Key
		}
		return result[i].ServiceID < result[j].ServiceID
	})
	return result, nil
}

// GetTask 查询指定服务下的自动化任务。
func (r *AutomationRegistry) GetTask(ctx context.Context, serviceID, taskKey string) (automation.Task, error) {
	service, err := r.getService(ctx, serviceID)
	if err != nil {
		return automation.Task{}, err
	}
	for _, task := range service.Tasks {
		if task.Key == strings.TrimSpace(taskKey) {
			if task.Endpoint == "" {
				task.Endpoint = joinURL(service.BaseURL, task.Key)
			}
			return task, nil
		}
	}
	return automation.Task{}, fmt.Errorf("automation task %s/%s not found", serviceID, taskKey)
}

// ResolveTask 将 gaia://service/task 解析成自动化服务和任务定义。
func (r *AutomationRegistry) ResolveTask(ctx context.Context, ref string) (automation.Service, automation.Task, error) {
	serviceID, taskKey, err := automation.ParseGaiaRef(ref)
	if err != nil {
		return automation.Service{}, automation.Task{}, err
	}
	service, err := r.getService(ctx, serviceID)
	if err != nil {
		return automation.Service{}, automation.Task{}, err
	}
	for _, task := range service.Tasks {
		if task.Key == taskKey {
			if task.Endpoint == "" {
				task.Endpoint = joinURL(service.BaseURL, task.Key)
			}
			return service, task, nil
		}
	}
	return automation.Service{}, automation.Task{}, fmt.Errorf("automation task %s/%s not found", serviceID, taskKey)
}

// ResolveEndpoint 将 gaia://service/task 解析成实际调用地址。
func (r *AutomationRegistry) ResolveEndpoint(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "gaia://") {
		return ref, nil
	}
	service, task, err := r.ResolveTask(ctx, ref)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(task.Endpoint) != "" {
		return task.Endpoint, nil
	}
	return joinURL(service.BaseURL, task.Key), nil
}

func (r *AutomationRegistry) getService(ctx context.Context, serviceID string) (automation.Service, error) {
	var model AutomationServiceModel
	if err := r.db.WithContext(ctx).Take(&model, "id = ?", strings.TrimSpace(serviceID)).Error; err != nil {
		return automation.Service{}, err
	}
	service, err := r.serviceFromModel(ctx, model)
	if err != nil {
		return automation.Service{}, err
	}
	if r.expired(service) {
		return automation.Service{}, fmt.Errorf("automation service %s not found", serviceID)
	}
	return service, nil
}

func (r *AutomationRegistry) serviceFromModel(ctx context.Context, model AutomationServiceModel) (automation.Service, error) {
	service, err := automationServiceFromModel(model)
	if err != nil {
		return automation.Service{}, err
	}
	var taskModels []AutomationTaskModel
	if err := r.db.WithContext(ctx).
		Where("service_id = ?", service.ID).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).
		Find(&taskModels).Error; err != nil {
		return automation.Service{}, err
	}
	service.Tasks = make([]automation.Task, 0, len(taskModels))
	for _, taskModel := range taskModels {
		task, err := automationTaskFromModel(taskModel)
		if err != nil {
			return automation.Service{}, err
		}
		service.Tasks = append(service.Tasks, task)
	}
	return service, nil
}

func (r *AutomationRegistry) expired(service automation.Service) bool {
	return service.TTLSeconds > 0 && r.now().After(service.UpdatedAt.Add(time.Duration(service.TTLSeconds)*time.Second))
}

func automationServiceToModel(service automation.Service) (AutomationServiceModel, error) {
	tagsJSON, err := json.Marshal(service.Tags)
	if err != nil {
		return AutomationServiceModel{}, err
	}
	return AutomationServiceModel{
		ID:           service.ID,
		Name:         service.Name,
		BaseURL:      service.BaseURL,
		HealthURL:    service.HealthURL,
		Protocol:     automation.NormalizeProtocol(service.Protocol),
		Version:      service.Version,
		TagsJSON:     tagsJSON,
		RegisteredAt: service.RegisteredAt,
		UpdatedAt:    service.UpdatedAt,
		TTLSeconds:   service.TTLSeconds,
	}, nil
}

func automationServiceFromModel(model AutomationServiceModel) (automation.Service, error) {
	var tags []string
	if len(model.TagsJSON) > 0 {
		if err := json.Unmarshal(model.TagsJSON, &tags); err != nil {
			return automation.Service{}, err
		}
	}
	return automation.Service{
		ID:           model.ID,
		Name:         model.Name,
		BaseURL:      model.BaseURL,
		HealthURL:    model.HealthURL,
		Protocol:     automation.NormalizeProtocol(model.Protocol),
		Version:      model.Version,
		Tags:         tags,
		RegisteredAt: model.RegisteredAt,
		UpdatedAt:    model.UpdatedAt,
		TTLSeconds:   model.TTLSeconds,
	}, nil
}

func automationTaskToModel(task automation.Task) (AutomationTaskModel, error) {
	inputJSON, err := json.Marshal(task.InputSchema)
	if err != nil {
		return AutomationTaskModel{}, err
	}
	outputJSON, err := json.Marshal(task.OutputSchema)
	if err != nil {
		return AutomationTaskModel{}, err
	}
	return AutomationTaskModel{
		ServiceID:        task.ServiceID,
		Key:              task.Key,
		Name:             task.Name,
		Description:      task.Description,
		Method:           task.Method,
		Endpoint:         task.Endpoint,
		InputSchemaJSON:  inputJSON,
		OutputSchemaJSON: outputJSON,
	}, nil
}

func automationTaskFromModel(model AutomationTaskModel) (automation.Task, error) {
	var input []automation.Parameter
	if len(model.InputSchemaJSON) > 0 {
		if err := json.Unmarshal(model.InputSchemaJSON, &input); err != nil {
			return automation.Task{}, err
		}
	}
	var output []automation.Parameter
	if len(model.OutputSchemaJSON) > 0 {
		if err := json.Unmarshal(model.OutputSchemaJSON, &output); err != nil {
			return automation.Task{}, err
		}
	}
	return automation.Task{
		ServiceID:    model.ServiceID,
		Key:          model.Key,
		Name:         model.Name,
		Description:  model.Description,
		Method:       model.Method,
		Endpoint:     model.Endpoint,
		InputSchema:  input,
		OutputSchema: output,
	}, nil
}

func joinURL(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	path = strings.TrimLeft(path, "/")
	if baseURL == "" || path == "" {
		return baseURL + path
	}
	return baseURL + "/" + path
}
