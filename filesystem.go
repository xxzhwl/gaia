/*
Package gaia 基础库
filesystem.go 文件操作的相关逻辑封装
@author wanlizhan
@created 2023-03-03
*/
package gaia

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const Sep = string(os.PathSeparator)

// FileExists 判断一个文件或目录是否存在
// @params string filename 待检查的文件名
// @return bool 如果存在返回true，否则返回false
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err != nil && !os.IsExist(err) {
		return false
	}
	return true
}

// FileBaseName 获取某个文件路径的具体文件名称
func FileBaseName(filename string) string {
	//首先将路径中的 \ 统一替换成 /
	filename = strings.Replace(filename, `\`, "/", -1)
	return path.Base(filename)
}

// FileRemoveSuffix 获取某个文件路径的具体文件名称且去除后缀格式
func FileRemoveSuffix(filename string) string {
	baseFileName := FileBaseName(filename)
	split := strings.Split(baseFileName, ".")
	return split[0]
}

// ReadLines 将一个文件中的内容读取到[]string中，过滤掉空行，以及每行的前后空格，并返回
// @params string filename 待读取的文件名
func ReadLines(filename string) ([]string, error) {
	retval := make([]string, 0)
	result, err := ReadLinesRaw(filename)
	if err != nil {
		return nil, err
	}
	if len(result) > 0 {
		for _, itm := range result {
			itm = strings.TrimSpace(itm)
			if len(itm) > 0 {
				retval = append(retval, itm)
			}
		}
	}
	return retval, nil

}

// ReadLinesRaw 将文件按行读入一个[]string数组中，不处理任何格式，比如空行，前后空格
func ReadLinesRaw(filename string) ([]string, error) {
	retval := make([]string, 0)
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			Log(LogErrorLevel, err.Error())
		}
	}()
	bufrd := bufio.NewReader(file)
	isEOF := false
	for {
		line, err := bufrd.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				isEOF = true
			} else {
				return nil, err
			}
		}
		retval = append(retval, line)
		if isEOF {
			break
		}
	}
	return retval, nil
}

// ReadFileAll 将整个文件读取到[]byte中
func ReadFileAll(filename string) ([]byte, error) {
	//文件路径安全性检查
	if err := FileSafetyCheck(filename, ""); err != nil {
		return nil, err
	}
	file, fileErr := os.Open(filename)
	if fileErr != nil {
		return nil, fileErr
	}
	defer func() {
		if err := file.Close(); err != nil {
			Log(LogErrorLevel, err.Error())
		}
	}()
	return io.ReadAll(file)
}

// ReadAll 读取数据，直到EOF结束，然后返回被读取的数据，如果读到EOF，error为nil
// 此函数的行为与 io.ReadAll 相同，但是可以指定初始化缓冲区大小
func ReadAll(r io.Reader, initBufferSize int) ([]byte, error) {
	b := make([]byte, 0, initBufferSize)
	for {
		if len(b) == cap(b) {
			// Add more capacity (let append pick how much).
			b = append(b, 0)[:len(b)]
		}
		n, err := r.Read(b[len(b):cap(b)])
		b = b[:len(b)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return b, err
		}
	}
}

// FileAppendContent 将数据写入到一个指定的文件中，并附加到文件内容的最后，如果文件不存在，则创建一个新文件
func FileAppendContent(filename, data string) error {
	file, fErr := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0755)
	if fErr != nil {
		return fErr
	}
	defer func() {
		if err := file.Close(); err != nil {
			Log(LogErrorLevel, err.Error())
		}
	}()
	if _, wErr := file.WriteString(data); wErr != nil {
		return wErr
	}
	return nil
}

// FileAppendBytes 将数据写入到一个指定的文件中，并附加到文件内容的最后，如果文件不存在，则创建一个新文件
func FileAppendBytes(filename string, data []byte) error {
	//文件路径安全性检查
	if err := FileSafetyCheck(filename, ""); err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			Log(LogErrorLevel, err.Error())
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return nil
}

// FilePutContent 将数据写入到一个文件中，如果文件已经存在，则覆盖已存在的文件
func FilePutContent(filename, data string) error {
	//路径安全检查
	if err := FileSafetyCheck(filename, ""); err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			Log(LogErrorLevel, err.Error())
		}
	}()
	if _, err := file.WriteString(data); err != nil {
		return err
	}
	return nil
}

// MkDirAll 创建指定路径的目录，如果目录已经存在，则忽略创建
// perm 指定格式如0777, 0755
func MkDirAll(pathname string, perm os.FileMode) error {
	if FileExists(pathname) {
		//目录已经存在
		return nil
	}
	return os.MkdirAll(pathname, perm)
}

// GetAllFilesInDir 获取某个目录下的所有文件名(全路径)，也包括子目录
func GetAllFilesInDir(dirPath string) ([]string, error) {
	if !FileExists(dirPath) {
		return nil, fmt.Errorf("directory(%s) is not found", dirPath)
	}
	files := make([]string, 0)
	walkErr := filepath.Walk(dirPath, func(fname string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		//不是目录，只选择文件，不选择目录
		if info.IsDir() {
			return nil
		}
		files = append(files, fname)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

// GetAllDirsInDir 获取某个目录下的所有子目录路径(全路径)，其中也包括传入的目录本身
// 这里仅返回目录，不返回文件，并且目录最后以 \ 或 / 结尾
func GetAllDirsInDir(dirPath string) ([]string, error) {
	if !FileExists(dirPath) {
		return nil, fmt.Errorf("directory(%s) is not found", dirPath)
	}
	dirs := make([]string, 0)
	ds := string(filepath.Separator)
	walkErr := filepath.Walk(dirPath, func(fname string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		dirs = append(dirs, fname+ds)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return dirs, nil
}

// FindFilepathInDir 提供一个目录和文件名(不带路径的)，在此目录以及所有的子目录中，查找此文件的完全路径(绝对路径)
// 只要在其中的一个目录中查到即返回，如果存在多个路径仅返回其中最先查找到的一个
// 如果没有查到，则返回为空
func FindFilepathInDir(dirPath string, filename string) (string, error) {
	if !FileExists(dirPath) {
		return "", fmt.Errorf("directory(%s) is not found", dirPath)
	}
	targetFilepath := ""
	ds := string(filepath.Separator)
	walkErr := filepath.Walk(dirPath, func(fname string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(fname, ds+filename) {
			targetFilepath = fname
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	return targetFilepath, nil
}

// FileSafetyCheck 文件路径以及文件名安全性检查，防止出现路径穿越漏洞
// filename 待检查的文件名完全路径
// fileSuffix 文件名后缀
func FileSafetyCheck(filename string, fileSuffix string) error {
	if len(fileSuffix) > 0 && !strings.HasSuffix(filename, fileSuffix) {
		//后缀名称不满足要求
		return fmt.Errorf("文件(%s)后缀不满足 %s 要求", filename, fileSuffix)
	}
	if strings.Contains(filename, "..") {
		return fmt.Errorf("文件(%s)路径中不允许出现 .. 符号", filename)
	}
	return nil
}

func GetCurrentAbPath() string {
	dir := GetCurrentAbPathByExecutable()
	tmpDir, _ := filepath.EvalSymlinks(os.TempDir())
	if strings.Contains(dir, tmpDir) {
		return GetCurrentAbPathByCaller()
	}
	return dir
}

// GetCurrentAbPathByExecutable 获取当前执行文件绝对路径
func GetCurrentAbPathByExecutable() string {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	res, _ := filepath.EvalSymlinks(filepath.Dir(exePath))
	return res
}

// GetCurrentAbPathByCaller 获取当前执行文件绝对路径（go run）
func GetCurrentAbPathByCaller() string {
	var abPath string
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		abPath = path.Dir(filename)
	}
	return abPath
}
