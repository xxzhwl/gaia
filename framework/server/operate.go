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
	// 对 insert/update：即使客户端未显式传 columns，schema 中声明了 InsertUseDefault/UpdateUseDefault
	// 的字段仍需补默认值，所以不能再用 len(c.Columns)!=0 作为短路守卫。
	// 对 delete：不关心 columns，无需处理。
	if c.OperateType == "insert" || c.OperateType == "update" {
		if c.Columns == nil {
			c.Columns = map[string]any{}
		}
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
	// 预建索引：sqlName -> CommonOperateColumn，用于 nullable 校验 O(1) 查找
	colBySqlName := make(map[string]CommonOperateColumn, len(c.schemaInfo.Columns))
	for _, column := range c.schemaInfo.Columns {
		colBySqlName[column.SqlName] = column
	}

	newColumns := map[string]any{}

	for _, column := range c.schemaInfo.Columns {
		if v, ok := c.Columns[column.Id]; ok {
			tempV := v
			var hErr error
			if column.InsertHandler != "" && c.OperateType == "insert" {
				tempV, hErr = c.valueHandler(v, column.InsertHandler)
				if hErr != nil {
					return hErr
				}
			}
			if column.UpdateHandler != "" && c.OperateType == "update" {
				tempV, hErr = c.valueHandler(v, column.UpdateHandler)
				if hErr != nil {
					return hErr
				}
			}
			newColumns[column.SqlName] = tempV
			continue
		}
		// 未在请求中出现：根据 useDefault 决定是否补默认值
		if column.InsertUseDefault && c.OperateType == "insert" {
			dv, dErr := c.valueHandler(column.DefaultValue, column.InsertHandler)
			if dErr != nil {
				return dErr
			}
			newColumns[column.SqlName] = dv
		}
		if column.UpdateUseDefault && c.OperateType == "update" {
			dv, dErr := c.valueHandler(column.DefaultValue, column.UpdateHandler)
			if dErr != nil {
				return dErr
			}
			newColumns[column.SqlName] = dv
		}
	}

	// 对最终列做 nullable 校验（O(N)）
	for k, v := range newColumns {
		if column, ok := colBySqlName[k]; ok {
			if !column.Nullable && isEmptyValue(v) {
				return errors.New(k + "不允许为空")
			}
		}
	}
	c.Columns = newColumns
	return nil
}

// isEmptyValue 判断是否为空值。
// 语义：
//   - nil 视为空
//   - 空字符串视为空
//   - 空 slice / 空 map 视为空
//   - 布尔 false 不算空
//   - 数值 0 不算空（0 是合法的库存 / 状态 / 序号值；过去把 0 视为空导致合法入库被拒）
func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case []any:
		return len(val) == 0
	case []string:
		return len(val) == 0
	case []int:
		return len(val) == 0
	case []int64:
		return len(val) == 0
	case []float64:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	case map[string]string:
		return len(val) == 0
	default:
		return false
	}
}

func (c *CommonOperateModel) valueHandler(columnValue any, handlerName string) (any, error) {
	if handlerName == "" {
		return columnValue, nil
	}
	handler, err := valueHandler.GetValueHandler(handlerName)
	if err != nil || handler == nil {
		return nil, fmt.Errorf("valueHandler [%s] not registered: %w", handlerName, err)
	}
	return handler.NewValue(columnValue), nil
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
		} else {
			gaia.WarnF("operate condition [%s] not found in schema [%s], dropped", k, c.schemaInfo.Schema)
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
		return nil, errors.New("schema 参数不能为空")
	}
	// 防路径穿越：校验 schema 名，与 loadOperateSchema 保持一致
	if err := validateSchemaName(query); err != nil {
		return nil, err
	}

	file, err := os.ReadFile(fmt.Sprintf(DefaultCommonOperateFileFmt, query))
	if err != nil {
		return nil, err
	}
	res := make(map[string]any)
	if err = json.Unmarshal(file, &res); err != nil {
		return nil, err
	}

	return res, nil
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
	if len(columns) == 0 {
		return 0, errors.New("请给出要插入的字段，禁止空插入")
	}
	db, err := d.db.DB()
	if err != nil {
		return 0, err
	}
	columnTemp := []string{}
	values := []any{}
	placeHolders := []string{}
	for key, v := range columns {
		columnTemp = append(columnTemp, "`"+key+"`")
		values = append(values, v)
		placeHolders = append(placeHolders, "?")
	}

	// 使用带 context 的 ExecContext，使 Insert 也能挂上 trace span。
	// 当 d.ctx 为 nil（调用方未设置）时退化为 context.Background()，避免 nil ctx panic。
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	exec, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO `%s` (%s) VALUES(%s)",
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
	if len(columns) == 0 {
		return 0, errors.New("请给出要更新的字段，禁止空更新")
	}
	newCondition, param := getConditionForOperate(condition)
	// 关键安全校验：所有条件都可能因不支持的操作符而被跳过，
	// 若处理后的条件为空则会产生无条件更新（UPDATE ... WHERE ），必须禁止。
	if len(newCondition) == 0 {
		return 0, errors.New("处理后的条件为空（可能使用了不支持的操作符），禁止无条件更新")
	}
	condExp := strings.Join(newCondition, " and ")

	tx := d.db.WithContext(d.ctx).Table(tableName).Where(condExp, param...).Updates(columns)
	if tx.Error != nil {
		return 0, tx.Error
	}

	return tx.RowsAffected, nil
}

func (d *DefaultWriter) Delete(tableName string, condition, extInfo map[string]any) (rows int64, err error) {
	// 必须使用处理后的 condition 进行校验，防止 checkCondition 过滤掉所有条件后导致无条件删除
	newCondition, param := getConditionForOperate(condition)
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

// parseOperator 把前端操作符映射为 SQL 操作符。
// 返回 sqlOp 为 "" 表示不支持该操作符，relatesNull 表示该操作符不需要占位参数。
func parseOperator(exp string) (sqlOp string, relatesNull bool) {
	switch exp {
	case "gte", ">=":
		return ">=", false
	case "lte", "<=":
		return "<=", false
	case "gt", ">":
		return ">", false
	case "lt", "<":
		return "<", false
	case "<>", "neq", "!=":
		return "<>", false
	case "eq", "=":
		return "=", false
	case "like", "Like":
		return "like", false
	case "is null", "is not null":
		return exp, true
	}
	return "", false
}

// getConditionForOperate 把 {key: value} 形式的条件转换为 GORM Where 可用的 SQL 片段和参数。
// 注意：key 已经由 CommonOperateModel.checkCondition 替换为 "<table>.<col>" 形式，这里不再需要主表名。
func getConditionForOperate(condition map[string]any) (
	newCondition []string, newConditionParam []any) {
	for key, vl := range condition {
		temp := key
		switch v := vl.(type) {
		case map[string]any:
			for exp, val := range v {
				expTemp, relateNull := parseOperator(exp)
				if expTemp == "" {
					continue
				}

				if relateNull {
					newCondition = append(newCondition, temp+" "+expTemp)
					continue
				}

				newCondition = append(newCondition, temp+" "+expTemp+" ?")
				newConditionParam = append(newConditionParam, val)
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
