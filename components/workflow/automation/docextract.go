package automation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
)

// MethodDoc 抽取指定 proxy 类型上指定方法的 doc 注释。
//
// 实现策略：
//  1. 通过反射拿到 proxy 类型的方法函数指针 → runtime.FuncForPC.FileLine 得到源文件路径；
//  2. 用 go/parser 解析该文件所在整个包（含指针/值接收者共同定义），构建 ast.Package；
//  3. 在包内查找 receiver 匹配 proxyTypeName、方法名匹配 methodName 的 FuncDecl，
//     提取其 Doc 注释并合并为单行描述。
//
// 若源码不可用（如二进制部署无源码），返回空串——调用方应按方法名 humanize 兜底。
// 结果按 (pkgDir, receiver, method) 三元组缓存，避免重复解析同一目录。
func MethodDoc(proxy any, methodName string) string {
	if proxy == nil || methodName == "" {
		return ""
	}
	value := reflect.ValueOf(proxy)
	if !value.IsValid() {
		return ""
	}
	typ := value.Type()
	if typ == nil {
		return ""
	}
	method, ok := typ.MethodByName(methodName)
	if !ok {
		return ""
	}

	// 拿到方法的源文件路径（可能是编译时嵌入的绝对路径）。
	fn := runtime.FuncForPC(method.Func.Pointer())
	if fn == nil {
		return ""
	}
	file, _ := fn.FileLine(method.Func.Pointer())
	if file == "" {
		return ""
	}
	pkgDir := filepath.Dir(file)

	// receiver 类型名（去掉 * 和包前缀）。
	recvType := typ
	if recvType.Kind() == reflect.Pointer {
		recvType = recvType.Elem()
	}
	receiver := recvType.Name()
	if receiver == "" {
		return ""
	}

	return lookupCachedDoc(pkgDir, receiver, methodName)
}

// docCacheKey 是 (pkgDir,receiver,method) 三元组。
type docCacheKey struct{ dir, recv, method string }

var (
	docMu    sync.RWMutex
	docCache = map[docCacheKey]string{}
	pkgMu    sync.Mutex
	pkgDocs  = map[string]map[string]map[string]string{} // dir -> receiver -> method -> doc
)

func lookupCachedDoc(pkgDir, receiver, method string) string {
	k := docCacheKey{pkgDir, receiver, method}
	docMu.RLock()
	if v, ok := docCache[k]; ok {
		docMu.RUnlock()
		return v
	}
	docMu.RUnlock()

	doc := loadPackageDocs(pkgDir)[receiver][method]

	docMu.Lock()
	docCache[k] = doc
	docMu.Unlock()
	return doc
}

// loadPackageDocs 解析 pkgDir 目录下的所有 .go 文件（不含测试文件），
// 返回 receiver -> method -> docString 索引。整个目录只解析一次。
func loadPackageDocs(pkgDir string) map[string]map[string]string {
	pkgMu.Lock()
	defer pkgMu.Unlock()
	if v, ok := pkgDocs[pkgDir]; ok {
		return v
	}
	result := map[string]map[string]string{}
	pkgDocs[pkgDir] = result

	fset := token.NewFileSet()
	filter := func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}
	pkgs, err := parser.ParseDir(fset, pkgDir, filter, parser.ParseComments)
	if err != nil {
		return result
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || fn.Doc == nil {
					continue
				}
				receiver := receiverTypeName(fn.Recv.List[0].Type)
				if receiver == "" {
					continue
				}
				methodName := fn.Name.Name
				doc := cleanDoc(fn.Doc.Text())
				if doc == "" {
					continue
				}
				if _, ok := result[receiver]; !ok {
					result[receiver] = map[string]string{}
				}
				result[receiver][methodName] = doc
			}
		}
	}
	return result
}

// receiverTypeName 从接收者语法节点抽出类型名（去掉 *）。
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// cleanDoc 把 go/doc 抽出的多行注释规整成单行描述，去掉方法名前缀。
//
// 约定：Go 官方注释以"方法名 ..."开头（如 "NormalizeOrder 归一化订单..."）。为了让展示更
// 简洁，剔除首个 token 若与方法名一致。
func cleanDoc(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 只取第一段（空行分段）以避免长篇大论进入描述。
	if idx := strings.Index(raw, "\n\n"); idx > 0 {
		raw = raw[:idx]
	}
	// 折叠多行为单行。
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}
