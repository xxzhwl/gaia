// Package server 包注释
// @author wanlizhan
// @created 2024/6/13
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/server/operateProxy"
	"github.com/xxzhwl/gaia/valueHandler"
)

/**
通用操作
*/

type CommonOperateSchema struct {
	Schema     string                   `json:"schema"`
	SchemaName string                   `json:"schema_name"`
	Author     string                   `json:"author"`
	DbSchema   string                   `json:"db_schema"`
	TableName  string                   `json:"table_name"`
	Writer     string                   `json:"writer"` //操作代理，为空时用默认的
	PrimaryKey string                   `json:"primary_key"`
	Columns    []CommonOperateColumn    `json:"columns"`
	Condition  []CommonOperateCondition `json:"condition"`
}

// CommonOperateColumn 通用操作每一个列
type CommonOperateColumn struct {
	Id               string   `json:"id"`
	Label            string   `json:"label"`
	SqlName          string   `json:"sql_name"`
	Example          any      `json:"example"`
	Nullable         bool     `json:"nullable"`
	DefaultValue     any      `json:"default_value"`
	InsertUseDefault bool     `json:"insert_use_default"`
	UpdateUseDefault bool     `json:"update_use_default"`
	InsertHandler    string   `json:"insert_handler"`
	UpdateHandler    string   `json:"update_handler"`
	Memo             string   `json:"memo"`
	DataType         string   `json:"data_type"`
	InputType        string   `json:"input_type"`
	Options          []Option `json:"options"`
	Hidden           bool     `json:"hidden"`
}

// CommonOperateCondition 通用操作每一个条件
type CommonOperateCondition struct {
	Id       string `json:"id"`
	Label    string `json:"label"`
	SqlName  string `json:"sql_name"`
	Example  string `json:"example"`
	Memo     string `json:"memo"`
	DataType string `json:"data_type"`
}

type CommonOperateModel struct {
	commonOperateModel

	schemaInfo CommonOperateSchema
	db         *gorm.DB //DB
	ctx        context.Context
}

type commonOperateModel struct {
	Schema      string         `json:"schema"` //操作的Schema
	OperateType string         `range:"insert;update;delete" json:"operate_type"`
	Columns     map[string]any `json:"columns"`   //需要操作的字段列表
	Condition   map[string]any `json:"condition"` //条件
	ExtInfo     map[string]any `json:"ext_info"`  //额外信息
}

func (c *CommonOperateModel) CommonOperate(req Request) (any, error) {
	c.ctx = req.TraceContext
	cTemp := commonOperateModel{}
	if err := req.BindJsonWithChecker(&cTemp); err != nil {
		return nil, err
	}
	c.commonOperateModel = cTemp

	schema, err := loadOperateSchema(c.Schema)
	if err != nil {
		return nil, err
	}
	c.schemaInfo = schema
	if len(c.Condition) != 0 {
		c.checkCondition()
	}
	if len(c.Columns) != 0 {
		if err := c.checkColumns(); err != nil {
			return nil, err
		}
	}
	switch c.OperateType {
	case "insert":
		return c.writerInsert(c.schemaInfo.DbSchema, c.schemaInfo.Writer)
	case "update":
		return c.writerUpdate(c.schemaInfo.DbSchema, c.schemaInfo.Writer)
	case "delete":
		return c.writerDelete(c.schemaInfo.DbSchema, c.schemaInfo.Writer)
	}
	return nil, errors.New("OperateType不符合预期[insert;update;delete]")
}

func (c *CommonOperateModel) checkColumns() error {
	newColumns := map[string]any{}

	for _, column := range c.schemaInfo.Columns {
		flag := false
		for k, v := range c.Columns {
			if k == column.Id {
				flag = true
				tempV := v
				if column.InsertHandler != "" && c.OperateType == "insert" {
					tempV = c.valueHandler(v, column.InsertHandler)
				}
				if column.UpdateHandler != "" && c.OperateType == "update" {
					tempV = c.valueHandler(v, column.UpdateHandler)
				}
				newColumns[column.SqlName] = tempV
				break
			}
		}

		if !flag && column.InsertUseDefault && c.OperateType == "insert" {
			newColumns[column.SqlName] = c.valueHandler(column.DefaultValue, column.InsertHandler)
		}
		if !flag && column.UpdateUseDefault && c.OperateType == "update" {
			newColumns[column.SqlName] = c.valueHandler(column.DefaultValue, column.UpdateHandler)
		}
	}

	for k, v := range newColumns {
		for _, column := range c.schemaInfo.Columns {
			if k == column.Id {
				if !column.Nullable && isEmptyValue(v) {
					return errors.New(k + "不允许为空")
				}
				break
			}
		}
	}
	gaia.PrettyString(newColumns)
	c.Columns = newColumns
	return nil
}

// isEmptyValue 判断是否为空值
// 注意：布尔类型的 false 不算空值
func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case int:
		return val == 0
	case int8:
		return val == 0
	case int16:
		return val == 0
	case int32:
		return val == 0
	case int64:
		return val == 0
	case uint:
		return val == 0
	case uint8:
		return val == 0
	case uint16:
		return val == 0
	case uint32:
		return val == 0
	case uint64:
		return val == 0
	case float32:
		return val == 0
	case float64:
		return val == 0
	case bool:
		return false // 布尔类型 false 不算空值
	default:
		return false
	}
}

func (c *CommonOperateModel) valueHandler(columnValue any, handlerName string) any {
	if handlerName == "" {
		return columnValue
	}
	handler, err := valueHandler.GetValueHandler(handlerName)
	if err != nil || handler == nil {
		gaia.Log(gaia.LogErrorLevel, fmt.Sprintf("GetInsertHandler[%s]Err:%s", handlerName, err))
		return columnValue
	}

	return handler.NewValue(columnValue)
}

func (c *CommonOperateModel) checkCondition() {
	conditionSqlName := map[string]string{}

	for _, condition := range c.schemaInfo.Condition {
		conditionSqlName[condition.Id] = c.schemaInfo.TableName + "." + condition.SqlName
	}

	newCondition := map[string]any{}
	for k, v := range c.Condition {
		if sqlK, ok := conditionSqlName[k]; ok {
			newCondition[sqlK] = v
		}
	}
	c.Condition = newCondition
}

func loadOperateSchema(schema string) (CommonOperateSchema, error) {
	// 校验 schema 名称，防止路径穿越
	if err := validateSchemaName(schema); err != nil {
		return CommonOperateSchema{}, err
	}

	return gaia.CacheLoad("common_operate_schema_"+schema, time.Minute*5, func() (CommonOperateSchema, error) {
		fileName := fmt.Sprintf(DefaultCommonOperateFileFmt, schema)
		exists := gaia.FileExists(fileName)
		if !exists {
			return CommonOperateSchema{}, errors.New(schema + " does not exist")
		}

		file, err := os.ReadFile(fileName)
		if err != nil {
			return CommonOperateSchema{}, err
		}
		c := CommonOperateSchema{}
		if err = json.Unmarshal(file, &c); err != nil {
			return CommonOperateSchema{}, err
		}
		return c, nil
	})
}

func (c *CommonOperateModel) GetAllCommonOperateSchema(req Request) (any, error) {
	dir, err := gaia.GetAllFilesInDir(DefaultCommonOperateFolder)
	if err != nil {
		return nil, err
	}
	for i, s := range dir {
		dir[i] = gaia.FileRemoveSuffix(s)
	}
	return dir, nil
}

func (c *CommonOperateModel) GetOperateSchemaDetail(req Request) (any, error) {
	query := req.GetUrlQuery("schema")
	if len(query) == 0 {
		return nil, fmt.Errorf("schema [%s] is not exist", query)
	}

	file, err := os.ReadFile(fmt.Sprintf(DefaultCommonOperateFileFmt, query))
	if err != nil {
		return nil, err
	}
	res := make(map[string]any)
	if err = json.Unmarshal(file, &res); err != nil {
		return nil, err
	}

	return map[string]any{"columns": res["columns"]}, nil
}

func (c *CommonOperateModel) writerInsert(dbSchema, writerName string) (lastId int64, err error) {
	proxy, err := operateProxy.GetOperateProxy(writerName)
	if err != nil {
		return 0, err
	}
	if err := proxy.SetDbSchema(dbSchema); err != nil {
		return 0, err
	}
	proxy.SetContext(c.ctx)
	return proxy.Insert(c.schemaInfo.TableName, c.Columns, c.ExtInfo)
}

func (c *CommonOperateModel) writerUpdate(dbSchema, writerName string) (rows int64, err error) {
	proxy, err := operateProxy.GetOperateProxy(writerName)
	if err != nil {
		return 0, err
	}
	if err := proxy.SetDbSchema(dbSchema); err != nil {
		return 0, err
	}
	proxy.SetContext(c.ctx)
	return proxy.Update(c.schemaInfo.TableName, c.Columns, c.Condition, c.ExtInfo)
}

func (c *CommonOperateModel) writerDelete(dbSchema, writerName string) (rows int64, err error) {
	proxy, err := operateProxy.GetOperateProxy(writerName)
	if err != nil {
		return 0, err
	}
	if err := proxy.SetDbSchema(dbSchema); err != nil {
		return 0, err
	}
	proxy.SetContext(c.ctx)
	return proxy.Delete(c.schemaInfo.TableName, c.Condition, c.ExtInfo)
}

type DefaultWriter struct {
	db  *gorm.DB
	ctx context.Context
}

func (d *DefaultWriter) SetContext(ctx context.Context) {
	d.ctx = ctx
}

func (d *DefaultWriter) SetDbSchema(dbSchema string) error {
	db, err := gaia.NewMysqlWithSchema(dbSchema)
	if err != nil {
		return err
	}
	d.db = db.GetGormDb()
	return nil
}

func (d *DefaultWriter) Insert(table string, columns, extInfo map[string]any) (lastId int64, err error) {
	db, err := d.db.DB()
	if err != nil {
		return 0, err
	}
	columnTemp := []string{}
	values := []any{}
	placeHolders := []string{}
	for key, v := range columns {
		columnTemp = append(columnTemp, key)
		values = append(values, v)
		placeHolders = append(placeHolders, "?")
	}

	exec, err := db.Exec(fmt.Sprintf("Insert Into %s (%s) VALUES(%s)",
		table, strings.Join(columnTemp, ","),
		strings.Join(placeHolders, ",")), values...)
	if err != nil {
		return 0, err
	}
	return exec.LastInsertId()
}

func (d *DefaultWriter) Update(tableName string, columns, condition, extInfo map[string]any) (rows int64,
	err error) {
	if len(condition) == 0 {
		return 0, errors.New("请给出条件，禁止无条件更新")
	}
	newCondition, param := getConditionForOperate(condition, tableName)
	condExp := strings.Join(newCondition, " and ")

	tx := d.db.WithContext(d.ctx).Table(tableName).Where(condExp, param...).Updates(columns)
	if tx.Error != nil {
		return 0, tx.Error
	}

	return tx.RowsAffected, nil
}

func (d *DefaultWriter) Delete(tableName string, condition, extInfo map[string]any) (rows int64, err error) {
	// 必须使用处理后的 condition 进行校验，防止 checkCondition 过滤掉所有条件后导致无条件删除
	newCondition, param := getConditionForOperate(condition, tableName)
	if len(newCondition) == 0 {
		return 0, errors.New("请给出条件，禁止无条件删除")
	}
	condExp := strings.Join(newCondition, " and ")

	tx := d.db.WithContext(d.ctx).Table(tableName).Where(condExp, param...).Delete(&map[string]any{})
	if tx.Error != nil {
		return 0, tx.Error
	}

	return tx.RowsAffected, nil
}

func getConditionForOperate(condition map[string]any, mainTable string) (
	newCondition []string, newConditionParam []any) {
	for key, vl := range condition {
		temp := key
		switch v := vl.(type) {
		case map[string]any:
			for exp, val := range v {
				expTemp := ""
				relateNull := false
				switch exp {
				case "gte", ">=":
					expTemp = ">="
				case "lte", "<=":
					expTemp = "<="
				case "gt", ">":
					expTemp = ">"
				case "lt", "<":
					expTemp = "<"
				case "<>", "neq", "!=":
					expTemp = "<>"
				case "eq", "=":
					expTemp = "="
				case "like", "Like":
					expTemp = "like"
				case "is null", "is not null":
					expTemp = exp
					relateNull = true
				}
				if expTemp == "" {
					continue
				}

				newCondition = append(newCondition, temp+" "+expTemp+" ?")
				if !relateNull {
					newConditionParam = append(newConditionParam, val)
				}
			}
		case []any:
			newCondition = append(newCondition, temp+" in ?")
			newConditionParam = append(newConditionParam, v)
		default:
			newCondition = append(newCondition, temp+" = ?")
			newConditionParam = append(newConditionParam, v)
		}
	}
	return
}
