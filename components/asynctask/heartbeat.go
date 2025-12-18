// Package asynctask 包注释
// @author wanlizhan
// @created 2024/5/7
package asynctask

import (
	"time"

	"github.com/xxzhwl/gaia"
	"gorm.io/gorm/clause"
)

const heatBeatTable = "async_task_heartbeat"

// HeartBeatModel 异步任务心跳结构
type HeartBeatModel struct {
	Id                int64
	TaskId            int64
	HeartBeatTime     time.Time
	HeartBeatNanoTime int64
}

// 检查断掉心跳且任务处于运行状态的任务，并将他们置为wait状态
func (s *Scheduler) heartBeat() {
	for {
		select {
		case <-s.exitContext.Done():
			gaia.InfoF("Received exit signal,AsyncTask heartBeat shutting down")
			return
		case <-time.After(time.Second * 5):
			taskIds, err := FindDeadTask()
			if err != nil {
				s.Logger.Error("查找心跳失活任务失败：" + err.Error())
				continue
			}
			if len(taskIds) == 0 {
				s.Logger.Info("查找心跳失活任务：无")
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

// FindDeadTask 查找死掉的任务
func FindDeadTask() ([]int64, error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, err
	}
	nano := time.Now().Add(-30 * time.Second)
	ids := []int64{}
	tx := db.GetGormDb().Table(heatBeatTable).Select("asynctasks.id").
		Joins("left join asynctasks on asynctasks.id = async_task_heartbeat.task_id").
		Where("async_task_heartbeat.heart_beat_nano_time < ?", nano.UnixNano()).
		Where("asynctasks.task_status = ?", TaskStatusRunning.String()).
		Find(&ids)
	if tx.Error != nil {
		return nil, tx.Error
	}
	otherIds := []int64{}
	findOther := db.GetGormDb().Table(taskTable).Select("id").Where("task_status = ?", TaskStatusRunning.String()).
		Where("update_time < ?", nano).Where(
		"id not in (select task_id from async_task_heartbeat)").Find(&otherIds)
	if findOther.Error != nil {
		return nil, tx.Error
	}
	return append(ids, otherIds...), nil
}

// UpdateDeadTaskToWait 将死掉的任务更新成为待运行状态
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

// InsertOrUpdateHeartBeat 插入心跳数据
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
	tx := db.GetGormDb().Table(heatBeatTable).Clauses(
		clause.OnConflict{Columns: []clause.Column{{Name: clause.PrimaryKey}},
			DoUpdates: clause.AssignmentColumns([]string{"heart_beat_time", "heart_beat_nano_time"})}).
		Create(&model)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}
