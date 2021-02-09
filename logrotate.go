// Package logrotate implements the io.Writer interface and rotates log file automatically.
// It can rotate log file by time or by file size.
// logrotate 包能自动进行日志文件切割，实现了 io.Writer 接口，
// 它可以按照时间或者文件大小切割。
package logrotate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"syscall"
	"time"
)

type File struct {
	rotateType     Type
	file           *os.File
	logPath        string
	rotateTime     time.Time
	nextRotateTime time.Time
	lock           *sync.RWMutex
}

// Type 代表切割类型
type Type int

const (
	None   Type = iota // 不切割日志
	BySize             // 按日志文件大小切割, ToDo 尚未实现
	ByHour             // 每小时切割日志
	ByDay              // 每天切割日志
)

// New 创建一个能自动进行日志切割的io.Writer实例，遇到失败返回错误
func New(logPath string, rotateType Type) (io.Writer, error) {
	if rotateType != None &&
		rotateType != ByHour &&
		rotateType != ByDay {
		return nil, errors.New("bad log rotate type")
	}

	now := time.Now()
	realLogPath := getRealLogPath(logPath, rotateType, now)

	if !fileExists(realLogPath) {
		logDir := path.Dir(realLogPath)
		if err := os.MkdirAll(logDir, os.ModeDir|0755); err != nil {
			return nil, err
		}
	}

	if fp, err := os.OpenFile(realLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666); err != nil {
		return nil, err
	} else {
		return &File{
			rotateType:     rotateType,
			file:           fp,
			logPath:        logPath,
			rotateTime:     now,
			nextRotateTime: getNextRotateTime(rotateType, now),
			lock:           new(sync.RWMutex),
		}, nil
	}
}

// NewMust 创建一个能自动进行日志切割的io.Writer实例，遇到失败返回os.Stderr
func NewMust(logPath string, rotateType Type) io.Writer {
	if file, err := New(logPath, rotateType); err == nil {
		fmt.Fprintf(os.Stderr, "logrotate.New fails, use stderr instead. err: %v", err)
		return file
	}
	return os.Stderr
}

// Write 实现 io.Writer 接口
func (f *File) Write(b []byte) (int, error) {
	if f.rotateType == None {
		return f.file.Write(b)
	}

	// 写入时刻，后续所有时间操作都以此为基准，避免时间调整、闰秒等因素影响出现细微的时间相关bug
	var now time.Time

	f.lock.RLock()
	now = time.Now()
	if now.Before(f.nextRotateTime) {
		defer f.lock.RUnlock()
		return f.file.Write(b)
	}
	f.lock.RUnlock()

	f.lock.Lock()
	defer f.lock.Unlock()

	// 获取写锁后再次检查时间
	if now.Before(f.nextRotateTime) {
		return f.file.Write(b)
	}

	// 关闭旧文件
	if f.file.Fd() > uintptr(syscall.Stderr) {
		f.file.Close()
	}

	// 打开新文件
	newRealLogPath := getRealLogPath(f.logPath, f.rotateType, now)
	if fp, err := os.OpenFile(newRealLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666); err != nil {
		fmt.Fprintf(os.Stderr, "logrotate open new file failed, use stderr instead. err: %v", err)
		f.file = os.Stderr
		f.rotateType = None
		//f.lock = nil
	} else {
		f.file = fp
	}

	f.rotateTime = now
	f.nextRotateTime = getNextRotateTime(f.rotateType, now)

	return f.file.Write(b)
}

// getNextRotateTime 计算下次切割时刻
func getNextRotateTime(rotateType Type, sinceTime time.Time) time.Time {
	if rotateType == ByHour {
		future := sinceTime.Add(time.Hour)
		return time.Date(future.Year(), future.Month(), future.Day(), future.Hour(), 0, 0, 0, sinceTime.Location())
	} else if rotateType == ByDay {
		future := sinceTime.Add(time.Hour * 24)
		return time.Date(future.Year(), future.Month(), future.Day(), 0, 0, 0, 0, sinceTime.Location())
	} else {
		// 一千年以后
		return time.Date(3000, 1, 1, 0, 0, 0, 0, sinceTime.Location())
	}
}

// getRealLogPath 获取日志文件真正的写入路径
func getRealLogPath(logPath string, rotateType Type, t time.Time) string {
	switch rotateType {
	case ByHour:
		return logPath + "." + t.Format("2006010215")
	case ByDay:
		return logPath + "." + t.Format("20060102")
	default:
		return logPath
	}
}

// fileExists 判断文件是否存在
func fileExists(path string) bool {
	fileInfo, err := os.Stat(path)
	return err == nil && fileInfo.Mode().IsRegular()
}
