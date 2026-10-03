package gormv2

import (
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB = gorm.DB
type Model = gorm.Model
type Association = gorm.Association
type DeletedAt = gorm.DeletedAt

// LogLevel 日志等级
type LogLevel = logger.LogLevel

const (
	LogLevelSilent = logger.Silent
	LogLevelError  = logger.Error
	LogLevelWarn   = logger.Warn
	LogLevelInfo   = logger.Info
)
