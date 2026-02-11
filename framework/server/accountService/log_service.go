package accountService

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"gorm.io/gorm"
)

// LogService 日志服务
type LogService struct {
	db *gorm.DB
}

// RecordLoginLogRequest 记录登录日志请求参数
type RecordLoginLogRequest struct {
	UserID     *int64
	Username   string
	LoginType  string
	IPAddress  string
	UserAgent  string
	DeviceType string
	OS         string
	Browser    string
	Location   string
	FailReason string
	Status     int8
}

// RecordOperationLogRequest 记录操作日志请求参数
type RecordOperationLogRequest struct {
	UserID    *int64
	Username  string
	Module    string
	Action    string
	Method    string
	Path      string
	IPAddress string
	UserAgent string
	ErrorMsg  string
	Duration  *int
	Status    int8
	Params    interface{}
	Result    interface{}
}

// GetLoginLogsRequest 获取登录日志列表请求参数
type GetLoginLogsRequest struct {
	Page     int
	PageSize int
	Filters  map[string]interface{}
}

// GetOperationLogsRequest 获取操作日志列表请求参数
type GetOperationLogsRequest struct {
	Page     int
	PageSize int
	Filters  map[string]interface{}
}

// NewLogService 创建日志服务实例
func NewLogService(db *gorm.DB) *LogService {
	return &LogService{db: db}
}

// RecordLoginLog 记录登录日志
func (s *LogService) RecordLoginLog(req *RecordLoginLogRequest) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	log := LoginLog{
		UserID:    req.UserID,
		Username:  &req.Username,
		LoginType: req.LoginType,
		IPAddress: &req.IPAddress,
		UserAgent: &req.UserAgent,
		Status:    req.Status,
	}

	// 设置可选字段
	if req.DeviceType != "" {
		log.DeviceType = &req.DeviceType
	}
	if req.OS != "" {
		log.OS = &req.OS
	}
	if req.Browser != "" {
		log.Browser = &req.Browser
	}
	if req.Location != "" {
		log.Location = &req.Location
	}
	if req.Status == 0 && req.FailReason != "" {
		log.FailReason = &req.FailReason
	}

	if err := s.db.WithContext(ctx).Create(&log).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("记录登录日志失败: %w", err))
	}

	return nil
}

// RecordOperationLog 记录操作日志
func (s *LogService) RecordOperationLog(req *RecordOperationLogRequest) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	log := OperationLog{
		UserID:   req.UserID,
		Username: &req.Username,
		Module:   &req.Module,
		Action:   &req.Action,
		Method:   &req.Method,
		Path:     &req.Path,
		Status:   req.Status,
		Duration: req.Duration,
	}

	// 设置IP地址和UserAgent
	if req.IPAddress != "" {
		log.IPAddress = &req.IPAddress
	}
	if req.UserAgent != "" {
		log.UserAgent = &req.UserAgent
	}

	// 序列化参数和结果
	if req.Params != nil {
		paramsJSON, err := json.Marshal(req.Params)
		if err != nil {
			return errwrap.Error(500, fmt.Errorf("序列化参数失败: %w", err))
		}
		paramsStr := string(paramsJSON)
		log.Params = &paramsStr
	}

	if req.Result != nil {
		resultJSON, err := json.Marshal(req.Result)
		if err != nil {
			return errwrap.Error(500, fmt.Errorf("序列化结果失败: %w", err))
		}
		resultStr := string(resultJSON)
		log.Result = &resultStr
	}

	// 设置错误信息
	if req.Status == 0 && req.ErrorMsg != "" {
		log.ErrorMsg = &req.ErrorMsg
	}

	if err := s.db.WithContext(ctx).Create(&log).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("记录操作日志失败: %w", err))
	}

	return nil
}

// GetLoginLogs 获取登录日志列表
func (s *LogService) GetLoginLogs(req *GetLoginLogsRequest) ([]LoginLog, int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var logs []LoginLog
	var total int64

	// 计算偏移量
	offset := (req.Page - 1) * req.PageSize

	// 构建查询条件
	query := s.db.WithContext(ctx).Model(&LoginLog{})

	// 应用过滤器
	if userID, ok := req.Filters["user_id"].(int64); ok {
		query = query.Where("user_id = ?", userID)
	}
	if username, ok := req.Filters["username"].(string); ok && username != "" {
		query = query.Where("username LIKE ?", "%"+username+"%")
	}
	if loginType, ok := req.Filters["login_type"].(string); ok && loginType != "" {
		query = query.Where("login_type = ?", loginType)
	}
	if ipAddress, ok := req.Filters["ip_address"].(string); ok && ipAddress != "" {
		query = query.Where("ip_address LIKE ?", "%"+ipAddress+"%")
	}
	if status, ok := req.Filters["status"].(int8); ok {
		query = query.Where("status = ?", status)
	}
	if startTime, ok := req.Filters["start_time"].(time.Time); ok {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime, ok := req.Filters["end_time"].(time.Time); ok {
		query = query.Where("created_at <= ?", endTime)
	}

	// 查询总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errwrap.Error(500, fmt.Errorf("查询登录日志总数失败: %w", err))
	}

	// 查询日志列表
	if err := query.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&logs).Error; err != nil {
		return nil, 0, errwrap.Error(500, fmt.Errorf("查询登录日志列表失败: %w", err))
	}

	return logs, total, nil
}

// GetOperationLogs 获取操作日志列表
func (s *LogService) GetOperationLogs(req *GetOperationLogsRequest) ([]OperationLog, int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var logs []OperationLog
	var total int64

	// 计算偏移量
	offset := (req.Page - 1) * req.PageSize

	// 构建查询条件
	query := s.db.WithContext(ctx).Model(&OperationLog{})

	// 应用过滤器
	if userID, ok := req.Filters["user_id"].(int64); ok {
		query = query.Where("user_id = ?", userID)
	}
	if username, ok := req.Filters["username"].(string); ok && username != "" {
		query = query.Where("username LIKE ?", "%"+username+"%")
	}
	if module, ok := req.Filters["module"].(string); ok && module != "" {
		query = query.Where("module LIKE ?", "%"+module+"%")
	}
	if action, ok := req.Filters["action"].(string); ok && action != "" {
		query = query.Where("action LIKE ?", "%"+action+"%")
	}
	if method, ok := req.Filters["method"].(string); ok && method != "" {
		query = query.Where("method = ?", method)
	}
	if path, ok := req.Filters["path"].(string); ok && path != "" {
		query = query.Where("path LIKE ?", "%"+path+"%")
	}
	if status, ok := req.Filters["status"].(int8); ok {
		query = query.Where("status = ?", status)
	}
	if startTime, ok := req.Filters["start_time"].(time.Time); ok {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime, ok := req.Filters["end_time"].(time.Time); ok {
		query = query.Where("created_at <= ?", endTime)
	}

	// 查询总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errwrap.Error(500, fmt.Errorf("查询操作日志总数失败: %w", err))
	}

	// 查询日志列表
	if err := query.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&logs).Error; err != nil {
		return nil, 0, errwrap.Error(500, fmt.Errorf("查询操作日志列表失败: %w", err))
	}

	return logs, total, nil
}

// GetLoginStats 获取登录统计信息
func (s *LogService) GetLoginStats(startTime, endTime time.Time) (map[string]interface{}, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	stats := make(map[string]interface{})

	// 总登录次数
	var totalLogins int64
	if err := s.db.WithContext(ctx).Model(&LoginLog{}).
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Count(&totalLogins).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取总登录次数失败: %w", err))
	}
	stats["total_logins"] = totalLogins

	// 成功登录次数
	var successLogins int64
	if err := s.db.WithContext(ctx).Model(&LoginLog{}).
		Where("created_at BETWEEN ? AND ? AND status = ?", startTime, endTime, 1).
		Count(&successLogins).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取成功登录次数失败: %w", err))
	}
	stats["success_logins"] = successLogins

	// 失败登录次数
	var failedLogins int64
	if err := s.db.WithContext(ctx).Model(&LoginLog{}).
		Where("created_at BETWEEN ? AND ? AND status = ?", startTime, endTime, 0).
		Count(&failedLogins).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取失败登录次数失败: %w", err))
	}
	stats["failed_logins"] = failedLogins

	// 按登录类型统计
	var loginTypeStats []struct {
		LoginType string
		Count     int64
	}
	if err := s.db.WithContext(ctx).Model(&LoginLog{}).
		Select("login_type, COUNT(*) as count").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Group("login_type").
		Scan(&loginTypeStats).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("按登录类型统计失败: %w", err))
	}
	stats["login_type_stats"] = loginTypeStats

	// 按日期统计（最近30天）
	var dailyStats []struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}
	if err := s.db.WithContext(ctx).Model(&LoginLog{}).
		Select("DATE(created_at) as date, COUNT(*) as count").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Group("DATE(created_at)").
		Scan(&dailyStats).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("按日期统计失败: %w", err))
	}
	stats["daily_stats"] = dailyStats

	// 失败原因统计
	var failReasonStats []struct {
		FailReason string `json:"fail_reason"`
		Count      int64  `json:"count"`
	}
	if err := s.db.WithContext(ctx).Model(&LoginLog{}).
		Select("fail_reason, COUNT(*) as count").
		Where("created_at BETWEEN ? AND ? AND status = ?", startTime, endTime, 0).
		Group("fail_reason").
		Scan(&failReasonStats).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("失败原因统计失败: %w", err))
	}
	stats["fail_reason_stats"] = failReasonStats

	return stats, nil
}

// GetOperationStats 获取操作统计信息
func (s *LogService) GetOperationStats(startTime, endTime time.Time) (map[string]interface{}, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	stats := make(map[string]interface{})

	// 总操作次数
	var totalOperations int64
	if err := s.db.WithContext(ctx).Model(&OperationLog{}).
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Count(&totalOperations).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取总操作次数失败: %w", err))
	}
	stats["total_operations"] = totalOperations

	// 成功操作次数
	var successOperations int64
	if err := s.db.WithContext(ctx).Model(&OperationLog{}).
		Where("created_at BETWEEN ? AND ? AND status = ?", startTime, endTime, 1).
		Count(&successOperations).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取成功操作次数失败: %w", err))
	}
	stats["success_operations"] = successOperations

	// 失败操作次数
	var failedOperations int64
	if err := s.db.WithContext(ctx).Model(&OperationLog{}).
		Where("created_at BETWEEN ? AND ? AND status = ?", startTime, endTime, 0).
		Count(&failedOperations).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取失败操作次数失败: %w", err))
	}
	stats["failed_operations"] = failedOperations

	// 按模块统计
	var moduleStats []struct {
		Module string `json:"module"`
		Count  int64  `json:"count"`
	}
	if err := s.db.WithContext(ctx).Model(&OperationLog{}).
		Select("module, COUNT(*) as count").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Group("module").
		Scan(&moduleStats).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("按模块统计失败: %w", err))
	}
	stats["module_stats"] = moduleStats

	// 按方法统计
	var methodStats []struct {
		Method string `json:"method"`
		Count  int64  `json:"count"`
	}
	if err := s.db.WithContext(ctx).Model(&OperationLog{}).
		Select("method, COUNT(*) as count").
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Group("method").
		Scan(&methodStats).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("按方法统计失败: %w", err))
	}
	stats["method_stats"] = methodStats

	// 平均响应时间
	var avgDuration float64
	if err := s.db.WithContext(ctx).Model(&OperationLog{}).
		Select("AVG(duration) as avg_duration").
		Where("created_at BETWEEN ? AND ? AND duration IS NOT NULL", startTime, endTime).
		Scan(&avgDuration).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取平均响应时间失败: %w", err))
	}
	stats["avg_duration"] = avgDuration

	// 最慢的10个操作
	var slowestOperations []OperationLog
	if err := s.db.WithContext(ctx).Model(&OperationLog{}).
		Where("created_at BETWEEN ? AND ? AND duration IS NOT NULL", startTime, endTime).
		Order("duration DESC").
		Limit(10).
		Find(&slowestOperations).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取最慢操作失败: %w", err))
	}
	stats["slowest_operations"] = slowestOperations

	return stats, nil
}

// CleanOldLogs 清理旧日志
func (s *LogService) CleanOldLogs(keepDays int) (map[string]int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if keepDays <= 0 {
		return nil, errwrap.Error(400, errors.New("保留天数必须大于0"))
	}

	cutoffTime := time.Now().AddDate(0, 0, -keepDays)

	result := make(map[string]int64)

	// 清理登录日志
	loginResult := s.db.WithContext(ctx).Where("created_at < ?", cutoffTime).Delete(&LoginLog{})
	if loginResult.Error != nil {
		return nil, errwrap.Error(500, fmt.Errorf("清理登录日志失败: %w", loginResult.Error))
	}
	result["login_logs"] = loginResult.RowsAffected

	// 清理操作日志
	operationResult := s.db.WithContext(ctx).Where("created_at < ?", cutoffTime).Delete(&OperationLog{})
	if operationResult.Error != nil {
		return nil, errwrap.Error(500, fmt.Errorf("清理操作日志失败: %w", operationResult.Error))
	}
	result["operation_logs"] = operationResult.RowsAffected

	return result, nil
}

// GetLogSummary 获取日志摘要
func (s *LogService) GetLogSummary() (map[string]interface{}, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	summary := make(map[string]interface{})

	// 今日登录统计
	todayStart := time.Now().Truncate(24 * time.Hour)
	todayEnd := todayStart.Add(24 * time.Hour)
	todayLoginStats, err := s.GetLoginStats(todayStart, todayEnd)
	if err != nil {
		return nil, err
	}
	summary["today_login_stats"] = todayLoginStats

	// 今日操作统计
	todayOperationStats, err := s.GetOperationStats(todayStart, todayEnd)
	if err != nil {
		return nil, err
	}
	summary["today_operation_stats"] = todayOperationStats

	// 最近7天登录趋势
	weekAgo := time.Now().AddDate(0, 0, -7)
	weekLoginStats, err := s.GetLoginStats(weekAgo, time.Now())
	if err != nil {
		return nil, err
	}
	summary["week_login_stats"] = weekLoginStats

	// 系统总用户数
	var totalUsers int64
	if err := s.db.WithContext(ctx).Model(&User{}).Count(&totalUsers).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取总用户数失败: %w", err))
	}
	summary["total_users"] = totalUsers

	// 活跃用户数（最近7天有登录的）
	var activeUsers int64
	if err := s.db.WithContext(ctx).Model(&LoginLog{}).
		Distinct("user_id").
		Where("created_at > ? AND status = ?", weekAgo, 1).
		Count(&activeUsers).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取活跃用户数失败: %w", err))
	}
	summary["active_users"] = activeUsers

	return summary, nil
}
