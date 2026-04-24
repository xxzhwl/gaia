// Package server 包注释
// @author wanlizhan
// @created 2024/6/13
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/dics"
)

/**
通用查询
*/

type CommonQuerySchema struct {
	Schema       string                 `json:"schema"`
	SchemaName   string                 `json:"schema_name"`
	Author       string                 `json:"author"`
	DbSchema     string                 `json:"db_schema"`
	TableName    string                 `json:"table_name"`
	DefaultLimit int64                  `json:"default_limit"`
	TimeFormat   string                 `json:"time_format"`
	PrimaryKey   string                 `json:"primary_key"`
	Columns      []CommonQueryColumn    `json:"columns"`
	Condition    []CommonQueryCondition `json:"condition"`
	Joins        []CommonQueryJoin      `json:"joins"`
}

type CommonQueryColumn struct {
	Id        string   `json:"id"`
	Label     string   `json:"label"`
	SqlName   string   `json:"sql_name"`
	DataType  string   `json:"data_type"`
	InputType string   `json:"input_type"`
	Options   []Option `json:"options"`
	JoinId    string   `json:"join_id"`
	Memo      string   `json:"memo"`
}

type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type CommonQueryCondition CommonQueryColumn

type CommonQueryJoin struct {
	Id           string `json:"id"`
	Join         string `json:"join"`
	BeforeJoinId string `json:"before_join_id"`
}

type SortKv struct {
	Name string `json:"name"`
	Desc bool   `json:"desc"`
}

type CommonQueryModel struct {
	commonQueryModel

	schemaInfo CommonQuerySchema
}

type commonQueryModel struct {
	Schema          string         `require:"1" json:"schema"`  //查询的Schema
	Columns         []string       `require:"1" json:"columns"` //需要查询的字段列表
	Condition       map[string]any `json:"condition"`           //查询条件
	IgnoreEmptyCond bool           `json:"ignore_empty_cond"`
	Start           int64          `gte:"0" json:"start"`
	Limit           int64          `gt:"0" json:"limit"`
	NeedRowNums     bool           `json:"need_row_nums"`
	Sort            []SortKv       `json:"sort"`
}

func (c *CommonQueryModel) CommonQuery(req Request) (any, error) {
	cTemp := commonQueryModel{}
	if err := req.BindJsonWithChecker(&cTemp); err != nil {
		return nil, err
	}
	c.commonQueryModel = cTemp
	schema, err := loadQuerySchema(c.Schema)
	if err != nil {
		return nil, err
	}
	c.schemaInfo = schema
	if c.Limit <= 0 {
		c.Limit = schema.DefaultLimit
	}
	if c.Limit <= 0 {
		c.Limit = 20
	}

	if len(cTemp.Condition) == 0 {
		return nil, errors.New("禁止无条件查询")
	}

	db, err := gaia.NewMysqlWithSchema(schema.DbSchema)
	if err != nil {
		return nil, err
	}
	//获取列对应sql、条件对应sql、join对应sql
	columnIdMap := make(map[string]string)
	condColumnIdMap := make(map[string]string)
	joinsIdMap := make(map[string]string)
	for _, column := range schema.Columns {
		if len(column.JoinId) != 0 {
			columnIdMap[column.Id] = column.JoinId + "." + column.SqlName
		} else {
			columnIdMap[column.Id] = schema.TableName + "." + column.SqlName
		}
	}

	for _, column := range schema.Condition {
		if len(column.JoinId) != 0 {
			condColumnIdMap[column.Id] = column.JoinId + "." + column.SqlName
		} else {
			condColumnIdMap[column.Id] = schema.TableName + "." + column.SqlName
		}
	}

	for _, column := range schema.Joins {
		joinsIdMap[column.Id] = column.Join
	}

	newColumns, joins := getSelectColumns(c.Columns, columnIdMap, schema.TableName)
	joins2, conditions, params := getCondition(c.Condition, condColumnIdMap, schema.TableName, c.IgnoreEmptyCond)
	if len(conditions) == 0 {
		return nil, errors.New("禁止无条件查询")
	}

	var joinBuilder strings.Builder
	joins = gaia.UniqueList(append(joins, joins2...))
	for _, join := range joins {
		joinBuilder.WriteByte(' ')
		joinBuilder.WriteString(joinsIdMap[join])
	}
	joinStr := joinBuilder.String()

	condExp := strings.Join(conditions, " and ")

	order := getOrder(c.Sort, columnIdMap)
	res := map[string]any{}
	var sum int64 = 0
	data := []map[string]any{}

	baseTx := db.GetGormDb().WithContext(req.TraceContext).Table(schema.TableName).
		Joins(joinStr).Where(condExp, params...)

	if c.NeedRowNums {
		if err := baseTx.Session(&gorm.Session{}).Count(&sum).Error; err != nil {
			gaia.Error(err.Error())
			return nil, err
		}
	}

	if err := baseTx.Session(&gorm.Session{}).Select(newColumns).
		Limit(int(c.Limit)).Offset(int(c.Start)).Order(order).
		Find(&data).Error; err != nil {
		gaia.Error(err.Error())
		return nil, err
	}
	if c.schemaInfo.TimeFormat != "" {
		// 优化：只遍历已知的列，而不是所有字段
		for i := range data {
			for _, column := range c.schemaInfo.Columns {
				if vTemp, ok := data[i][column.Id].(time.Time); ok {
					data[i][column.Id] = vTemp.Format(c.schemaInfo.TimeFormat)
				}
			}
		}
	}
	res["data"] = data
	res["sum"] = sum

	return res, nil
}

func loadQuerySchema(schema string) (CommonQuerySchema, error) {
	// 校验 schema 名称，防止路径穿越
	if err := validateSchemaName(schema); err != nil {
		return CommonQuerySchema{}, err
	}

	return gaia.CacheLoad("common_query_schema_"+schema, time.Minute*5, func() (CommonQuerySchema, error) {
		fileName := fmt.Sprintf(DefaultCommonQueryFileFmt, schema)
		exists := gaia.FileExists(fileName)
		if !exists {
			return CommonQuerySchema{}, errors.New(schema + " does not exist")
		}

		file, err := os.ReadFile(fileName)
		if err != nil {
			return CommonQuerySchema{}, err
		}
		c := CommonQuerySchema{}
		if err = json.Unmarshal(file, &c); err != nil {
			return CommonQuerySchema{}, err
		}
		return c, nil
	})
}

func getSelectColumns(columns []string, columnIdMap map[string]string, mainTable string) (newColumns []string, joins []string) {
	for _, column := range columns {
		if v, ok := columnIdMap[column]; ok {
			split := strings.Split(v, ".")
			if len(split) != 2 {
				continue
			}
			if split[0] != mainTable {
				joins = append(joins, split[0])
			}
			newColumns = append(newColumns, v)
		}
	}
	return newColumns, joins
}

func getCondition(condition map[string]any, condColumnIdMap map[string]string, mainTable string,
	ignoreEmpty bool) (joins []string,
	newCondition []string, newConditionParam []any) {
	for key, v := range condition {
		temp := ""

		if vt, ok := condColumnIdMap[key]; !ok {
			gaia.WarnF("query condition [%s] not found in schema, dropped", key)
			continue
		} else {
			split := strings.Split(vt, ".")
			if len(split) != 2 {
				continue
			}
			if split[0] != mainTable {
				joins = append(joins, split[0])
			}
			temp = vt
		}

		switch vv := v.(type) {
		case map[string]any:
			for exp, val := range vv {
				expTemp, relateNull := parseOperator(exp)
				if expTemp == "" {
					gaia.WarnF("query condition operator [%s] not supported, skipped", exp)
					continue
				}

				// 统一支持 ignoreEmpty：忽略空值的操作符条件
				if ignoreEmpty && isEmptyValue(val) {
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
			newConditionParam = append(newConditionParam, vv)
		default:
			if !(ignoreEmpty && isEmptyValue(vv)) {
				newCondition = append(newCondition, temp+" = ?")
				newConditionParam = append(newConditionParam, vv)
			}
		}
	}
	return
}

// isSafeSqlIdentifierPath 校验形如 "table.col" 或 "col" 的标识符是否安全：
// 只允许 [a-zA-Z0-9_.]，长度不超过 128。
// 深度防御：即便 schema JSON 文件被写脏，也不会把非法字符拼进 ORDER BY。
func isSafeSqlIdentifierPath(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func getOrder(kvs []SortKv, columnIdMap map[string]string) string {
	res := []string{}
	for _, kv := range kvs {
		v, ok := columnIdMap[kv.Name]
		if !ok {
			continue
		}
		// 深度防御：ORDER BY 直接字符串拼接进 SQL（GORM 不做参数化），
		// 必须确保字段名是纯标识符。
		if !isSafeSqlIdentifierPath(v) {
			continue
		}
		if kv.Desc {
			res = append(res, v+" desc")
		} else {
			res = append(res, v+" asc")
		}
	}
	return strings.Join(res, ",")
}

func (c *CommonQueryModel) GetAllCommonQuerySchema(req Request) (any, error) {
	dir, err := gaia.GetAllFilesInDir(DefaultCommonQueryFolder)
	if err != nil {
		return nil, err
	}
	for i, s := range dir {
		dir[i] = gaia.FileRemoveSuffix(s)
	}
	return dir, nil
}

func (c *CommonQueryModel) GetQuerySchemaDetail(req Request) (any, error) {
	query := req.GetUrlQuery("schema")
	if len(query) == 0 {
		return nil, errors.New("schema 参数不能为空")
	}
	// 防路径穿越：校验 schema 名，与 loadQuerySchema 保持一致
	if err := validateSchemaName(query); err != nil {
		return nil, err
	}

	file, err := os.ReadFile(fmt.Sprintf(DefaultCommonQueryFileFmt, query))
	if err != nil {
		return nil, err
	}
	res := make(map[string]any)
	if err = json.Unmarshal(file, &res); err != nil {
		return nil, err
	}

	return res, nil
}

// validateTableName 校验表名是否合法，防止 SQL 注入
func validateTableName(table string) error {
	if len(table) == 0 {
		return errors.New("表名不能为空")
	}
	// 只允许字母、数字、下划线
	for _, r := range table {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return errors.New("表名只能包含字母、数字和下划线")
		}
	}
	return nil
}

// validateSchemaName 校验 schema 名称是否合法，防止路径穿越
func validateSchemaName(schema string) error {
	if len(schema) == 0 {
		return errors.New("schema 不能为空")
	}
	// 禁止包含路径分隔符和特殊字符
	if strings.Contains(schema, "..") || strings.Contains(schema, "/") || strings.Contains(schema, "\\") {
		return errors.New("schema 名称不合法")
	}
	// 只允许字母、数字、下划线和连字符
	for _, r := range schema {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return errors.New("schema 名称只能包含字母、数字、下划线和连字符")
		}
	}
	return nil
}

// validateConfigKey 校验配置 key 是否合法（gaia 配置路径形如 Framework.Mysql、Business.Sub.Key）
// 允许字母、数字、下划线、连字符、点号；不允许 ".."、"/"、"\"
func validateConfigKey(key string) error {
	if len(key) == 0 {
		return errors.New("配置 key 不能为空")
	}
	if strings.Contains(key, "..") || strings.Contains(key, "/") || strings.Contains(key, "\\") {
		return errors.New("配置 key 不合法")
	}
	for _, r := range key {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return errors.New("配置 key 只能包含字母、数字、下划线、连字符和点号")
		}
	}
	return nil
}

func generateCommon(req Request) (any, error) {
	dbSchema := req.GetUrlQuery("dbSchema")
	table := req.GetUrlQuery("table")

	// 校验 dbSchema（gaia 配置 key），允许点号（如 "Framework.Mysql"）但防止路径穿越
	if err := validateConfigKey(dbSchema); err != nil {
		return nil, fmt.Errorf("dbSchema 校验失败: %w", err)
	}
	// 校验表名，防止 SQL 注入
	if err := validateTableName(table); err != nil {
		return nil, err
	}

	db, err := gaia.NewMysqlWithSchema(dbSchema)
	if err != nil {
		return nil, err
	}
	qs, ops := CommonQuerySchema{
		Schema:       table,
		SchemaName:   table,
		DbSchema:     dbSchema,
		TableName:    table,
		TimeFormat:   "2006-01-02 15:04:05",
		DefaultLimit: 5000,
		Columns:      make([]CommonQueryColumn, 0),
		Condition:    make([]CommonQueryCondition, 0),
		Joins:        make([]CommonQueryJoin, 0),
	}, CommonOperateSchema{
		Schema:     table,
		SchemaName: table,
		DbSchema:   dbSchema,
		TableName:  table,
		Writer:     "default",
		Columns:    make([]CommonOperateColumn, 0),
		Condition:  make([]CommonOperateCondition, 0),
	}

	// 使用反引号包裹表名，防止 SQL 注入
	tx := db.GetGormDb().WithContext(req.TraceContext).Raw("show full columns from `" + table + "`")
	rows, err := tx.Rows()
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, errors.New("desc table nil")
	}
	defer rows.Close()

	fetch, err := gaia.MysqlFetch(rows)
	if err != nil {
		return nil, err
	}
	for _, row := range fetch {
		label := dics.S(row, "Field")
		isPri := false
		if dics.S(row, "Key") == "PRI" {
			qs.PrimaryKey = label
			ops.PrimaryKey = label
			isPri = true
		}

		memo := ""
		comment := dics.S(row, "Comment")
		options := []Option{}
		inputType := "text"
		if comment != "" {
			labelTemp, memoTemp := extractBracketContent(comment)
			if len(labelTemp) > 0 {
				label = fmt.Sprintf("%s(%s)", labelTemp, label)
			}
			memo = memoTemp
			if len(memo) > 0 {
				options = extractCondOptions(memo)
				if len(options) > 0 {
					inputType = "select"
				}
			}
		}
		if strings.Contains(dics.S(row, "Type"), "datetime") {
			inputType = "time"
		}
		qs.Columns = append(qs.Columns, CommonQueryColumn{
			Id:        dics.S(row, "Field"),
			Label:     label,
			SqlName:   dics.S(row, "Field"),
			DataType:  dics.S(row, "Type"),
			Memo:      memo,
			JoinId:    "",
			InputType: inputType,
			Options:   options,
		})
		qs.Condition = append(qs.Condition, CommonQueryCondition{
			Id:        dics.S(row, "Field"),
			Label:     label,
			SqlName:   dics.S(row, "Field"),
			DataType:  dics.S(row, "Type"),
			Memo:      memo,
			JoinId:    "",
			InputType: inputType,
			Options:   options,
		})
		handler := ""
		if strings.Contains(dics.S(row, "Type"), "datetime") {
			handler = "Time"
		}
		var defaultValue = getDefaultValue(dics.S(row, "Type"))
		var canEmpty = false
		var insertUseDefault = true
		if dics.S(row, "Null") == "YES" && !isPri {
			canEmpty = true
			insertUseDefault = false
		}
		ops.Columns = append(ops.Columns, CommonOperateColumn{
			Id:               dics.S(row, "Field"),
			Label:            label,
			SqlName:          dics.S(row, "Field"),
			DataType:         dics.S(row, "Type"),
			Nullable:         canEmpty,
			DefaultValue:     defaultValue,
			InsertUseDefault: insertUseDefault,
			UpdateUseDefault: false,
			Example:          defaultValue,
			InsertHandler:    handler,
			UpdateHandler:    "",
			Memo:             memo,
			InputType:        inputType,
			Options:          options,
		})
		ops.Condition = append(ops.Condition, CommonOperateCondition{
			Id:       dics.S(row, "Field"),
			Label:    label,
			SqlName:  dics.S(row, "Field"),
			DataType: dics.S(row, "Type"),
			Example:  "",
			Memo:     memo,
		})
	}
	qsContent, err := gaia.PrettyString(qs)
	if err != nil {
		return nil, err
	}
	osContent, err := gaia.PrettyString(ops)
	if err != nil {
		return nil, err
	}

	qsFileName := fmt.Sprintf(DefaultCommonQueryFileFmt, table)
	osFileName := fmt.Sprintf(DefaultCommonOperateFileFmt, table)

	// 安全：默认不覆盖已存在的 schema 文件，避免误操作冲掉已调优过的配置。
	// 如需强制覆盖，显式传 ?force=1
	force := req.GetUrlQuery("force") == "1"
	if !force {
		if gaia.FileExists(qsFileName) {
			return nil, fmt.Errorf("query schema 已存在: %s（传 force=1 覆盖）", qsFileName)
		}
		if gaia.FileExists(osFileName) {
			return nil, fmt.Errorf("operate schema 已存在: %s（传 force=1 覆盖）", osFileName)
		}
	}

	if err := gaia.FilePutContent(qsFileName, qsContent); err != nil {
		return nil, err
	}
	if err := gaia.FilePutContent(osFileName, osContent); err != nil {
		return nil, err
	}
	// schema 文件已更新，主动清除 5 分钟缓存，使下一次请求立即生效
	cache := gaia.NewCache()
	cache.Delete("common_query_schema_" + table)
	cache.Delete("common_operate_schema_" + table)
	return map[string]any{"QueryContent": qsContent, "OperateContent": osContent}, nil
}

func getDefaultValue(valueType string) any {
	if strings.Contains(valueType, "int") {
		return 0
	}
	if strings.Contains(valueType, "float") {
		return 0.0
	}
	if strings.Contains(valueType, "char") || strings.Contains(valueType, "text") {
		return ""
	}
	return ""
}

func extractBracketContent(s string) (string, string) {
	start := strings.Index(s, "[")
	end := strings.Index(s, "]")

	// 检查是否找到中括号且顺序正确
	if start == -1 || end == -1 || start >= end {
		return s, s
	}

	return s[0:start], s[start+1 : end]
}

func extractCondOptions(s string) (options []Option) {
	split := strings.Split(s, ";")
	if len(split) == 1 {
		return
	}
	for _, temp := range split {
		i := strings.Split(temp, ":")
		if len(i) == 2 {
			options = append(options, Option{
				Label: i[1],
				Value: i[0],
			})
		}
	}
	return
}
