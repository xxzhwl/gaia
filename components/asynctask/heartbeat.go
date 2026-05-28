// Package asynctask 包注释
// @author wanlizhan
// @created 2024/5/7
package asynctask

import (
	"time"

	"github.com/xxzhwl/gaia"
	"gorm.io/gorm/clause"
)

const heartBeatTable = "async_task_heartbeat"

type HeartBeatModel struct {
	Id                int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TaskId            int64     `gorm:"column:task_id;not null;uniqueIndex:idx_async_task_heartbeat_task_id"`
	HeartBeatTime     time.Time `gorm:"column:heart_beat_time"`
	HeartBeatNanoTime int64     `gorm:"column:heart_beat_nano_time;default:0"`
}

func (HeartBeatModel) TableName() string { return heartBeatTable }

func (s *Scheduler) heartBeat() {
	gaia.BuildContextTrace()
	for {
		select {
		case <-s.exitContext.Done():
			gaia.InfoF("Received exit signal,AsyncTask heartBeat shutting down")
			return
		case <-s.stopContext.Done():
			s.Logger.Info("HeartBeat received stop signal, shutting down")
			return
		case <-time.After(s.HeartBeatInterval):
			taskIds, err := FindDeadTask()
			if err != nil {
				s.Logger.Error("查找心跳失活任务失败：" + err.Error())
				continue
			}
			if len(taskIds) == 0 {
				s.Logger.Debug("查找心跳失活任务：无")
				continue
			} else {
				s.Logger.InfoF("检查到心跳失活任务列表%v，即将更新他们的状态到等待状态", taskIds)
				if err = UpdateDeadTaskToWait(taskIds); err != nil {
					s.Logger.Error("将失败任务更新为wait-ERR:" + err.Error())
					continue
				}
			}
		}
	}
}

func FindDeadTask() ([]int64, error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-30 * time.Second)
	cutoffNano := cutoff.UnixNano()

	// 心跳记录已存在但心跳时间过老 → 视为失活
	staleHeartbeat := []int64{}
	tx := db.GetGormDb().Table(heartBeatTable).Select("asynctasks.id").
		Joins("JOIN asynctasks ON asynctasks.id = async_task_heartbeat.task_id").
		Where("async_task_heartbeat.heart_beat_nano_time < ?", cutoffNano).
		Where("asynctasks.task_status = ?", TaskStatusRunning.String()).
		Find(&staleHeartbeat)
	if tx.Error != nil {
		return nil, tx.Error
	}

	// 完全没有心跳记录但更新时间也老 → 视为失活（用 LEFT JOIN ... IS NULL 替代 NOT IN 子查询，性能更可控）
	noHeartbeat := []int64{}
	tx2 := db.GetGormDb().Table(taskTable+" AS t").Select("t.id").
		Joins("LEFT JOIN async_task_heartbeat AS h ON h.task_id = t.id").
		Where("t.task_status = ?", TaskStatusRunning.String()).
		Where("t.update_time < ?", cutoff).
		Where("h.task_id IS NULL").
		Find(&noHeartbeat)
	if tx2.Error != nil {
		return nil, tx2.Error
	}
	return append(staleHeartbeat, noHeartbeat...), nil
}

func UpdateDeadTaskToWait(taskIds []int64) error {
	if len(taskIds) <= 0 {
		return nil
	}
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}
	tx := db.GetGormDb().Table(taskTable).Where("id In ? and task_status in ?", taskIds, []string{TaskStatusRunning.String()}).
		Update("task_status", TaskStatusWait.String())
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}

func InsertOrUpdateHeartBeat(taskId int64) error {
	if taskId <= 0 {
		return nil
	}
	now := time.Now()
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}
	model := HeartBeatModel{
		TaskId:            taskId,
		HeartBeatTime:     now,
		HeartBeatNanoTime: now.UnixNano(),
	}
	tx := db.GetGormDb().Table(heartBeatTable).Clauses(
		clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"heart_beat_time", "heart_beat_nano_time"})}).
		Create(&model)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}
