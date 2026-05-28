// Package jobs 高可用支持：实例ID + 租约抢占 + 心跳 + 僵尸自愈。
// @author wanlizhan
// @created 2026/05/28
//
// 设计要点：
//   - 单进程内 robfig/cron 仍然各自计时，到点后通过对 job_center 的 *原子* UPDATE
//     抢占租约，胜出者才执行；其他副本静默放弃。
//   - 任务执行期间起独立 goroutine 续 lease_expire_at 与 last_heartbeat_time。
//   - 进程崩溃 / 网络分区下，租约会自动到期，下一次 cron 触发时由其它副本抢回，
//     从而实现僵尸自愈，无需人工介入。
//   - 不引入 Redis / Etcd 等外部依赖，仅依赖现有 MySQL。
package jobs

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
)

// HA 默认参数（可通过 RunJob.WithHA* 进行覆写）。
const (
	// DefaultLeaseGraceFactor 决定 lease_expire_at = max(timeout, 60s) * factor。
	// 默认设置 1.5 倍：足以覆盖业务超时 + 心跳抖动。
	DefaultLeaseGraceFactor = 1.5

	// DefaultLeaseHeartbeatInterval 心跳续租间隔。
	DefaultLeaseHeartbeatInterval = 10 * time.Second

	// DefaultMinLeaseDuration 最小租约时长，防止 timeout 极小导致误判。
	DefaultMinLeaseDuration = 60 * time.Second

	// DefaultStopWaitTimeout Stop() 等待运行中任务结束的最大时长。
	DefaultStopWaitTimeout = 30 * time.Second
)

var (
	// hostInstanceId 是当前进程的全局唯一标识，启动时生成一次。
	hostInstanceId   string
	hostInstanceOnce sync.Once
)

// GetInstanceId 返回当前进程的 jobs 模块实例ID。
//
// 形如 "<hostname>-<pid>-<nanoTs>"。
// 用作 job_center.lease_owner / job_record.instance_id，便于运维定位"是谁跑的"。
func GetInstanceId() string {
	hostInstanceOnce.Do(func() {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown"
		}
		hostInstanceId = fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())
	})
	return hostInstanceId
}

// computeLeaseDuration 根据 job 的 timeout 推算合理的租约时长。
// 返回值至少为 DefaultMinLeaseDuration。
func computeLeaseDuration(j JobBase) time.Duration {
	timeout := time.Duration(j.GetTimeout()) * time.Second
	if timeout <= 0 {
		timeout = DefaultMinLeaseDuration
	}
	d := time.Duration(float64(timeout) * DefaultLeaseGraceFactor)
	if d < DefaultMinLeaseDuration {
		d = DefaultMinLeaseDuration
	}
	return d
}

// tryAcquireJobLease 尝试抢占某 job 的执行租约（多副本互斥）。
//
// 抢占规则：
//   - 任务必须 enabled=1。
//   - 满足任一即可抢：(a) run_status='待运行'；(b) 当前租约已过期（lease_expire_at IS NULL 或 < NOW(3)）。
//   - 抢占成功时同时把状态置为运行中，记录本实例为 owner，并刷新心跳。
//
// 返回 true 代表本副本拿到了执行权；false 表示被其它副本抢先 / 仍在租约内。
func (r *RunJob) tryAcquireJobLease(ctx context.Context, jobId int64, leaseDur time.Duration) (bool, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return false, err
	}
	now := time.Now()
	expireAt := now.Add(leaseDur)
	owner := GetInstanceId()

	// 注意：直接用 SQL UPDATE 的 RowsAffected 判断胜出者，依赖 InnoDB 行锁。
	tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Where("id = ?", jobId).
		Where("enabled = ?", true).
		Where(
			db.GetGormDb().Where("run_status = ?", RunStatusWait).
				Or("lease_expire_at IS NULL").
				Or("lease_expire_at < ?", now),
		).
		Updates(map[string]interface{}{
			"run_status":          RunStatusRunning,
			"lease_owner":         owner,
			"lease_expire_at":     expireAt,
			"last_heartbeat_time": now,
			"last_run_time":       now,
			"update_time":         now,
		})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected == 1, nil
}

// renewJobLease 续租。
//
// 仅当当前 owner 仍是本实例时才生效；防止"租约已被别人抢走"后误续。
func (r *RunJob) renewJobLease(ctx context.Context, jobId int64, leaseDur time.Duration) (bool, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return false, err
	}
	now := time.Now()
	tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Where("id = ?", jobId).
		Where("lease_owner = ?", GetInstanceId()).
		Updates(map[string]interface{}{
			"lease_expire_at":     now.Add(leaseDur),
			"last_heartbeat_time": now,
		})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected == 1, nil
}

// releaseJobLease 释放本实例持有的租约。
//
// 仅当 lease_owner = 本实例 时才会清理，避免覆盖其它副本的并行执行。
// runStatus 用于决定释放后任务的 run_status；通常传 RunStatusWait。
func (r *RunJob) releaseJobLease(ctx context.Context, jobId int64, runStatus string) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	now := time.Now()
	tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Where("id = ?", jobId).
		Where("lease_owner = ?", GetInstanceId()).
		Updates(map[string]interface{}{
			"run_status":      runStatus,
			"lease_owner":     "",
			"lease_expire_at": nil,
			"update_time":     now,
		})
	return tx.Error
}

// startLeaseHeartbeat 在任务执行期间开启心跳续租 goroutine。
//
// 返回 cancel：调用者在任务执行结束后必须调用以停止心跳。
// 心跳失败不会中断任务执行，仅写错误日志 + DBError 计数。
func (r *RunJob) startLeaseHeartbeat(parentCtx context.Context, jobId int64, leaseDur time.Duration) context.CancelFunc {
	hbCtx, cancel := context.WithCancel(parentCtx)
	r.runningWg.Add(1)
	go func() {
		defer r.runningWg.Done()
		ticker := time.NewTicker(DefaultLeaseHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				ok, err := r.renewJobLease(context.Background(), jobId, leaseDur)
				if err != nil {
					r.counters.dbError.Add(1)
					recordJobDBError(context.Background(), "lease_renew")
					r.cronJobLogger.ErrorF("续租 job %d 心跳失败: %s", jobId, err.Error())
					continue
				}
				recordLeaseRenew(context.Background(), ok)
				if !ok {
					// 已被别的副本抢走（说明本节点心跳卡顿过久），主动退出心跳循环
					r.cronJobLogger.WarnF("job %d 租约已被其他实例接管，停止本实例心跳", jobId)
					return
				}
			}
		}
	}()
	return cancel
}

// reclaimMyOrphanLeases 启动时回收：曾经由"上一代本机进程"持有但未释放的租约。
//
// 仅匹配 hostname 前缀（同机重启的场景），把 stale 租约清理掉，让任务尽快被调度。
// 注意：不会跨主机清理，跨主机僵尸由租约到期自动恢复。
func (r *RunJob) reclaimMyOrphanLeases(ctx context.Context) error {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return nil
	}
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	now := time.Now()
	// 仅清理"同机但不同进程"的旧租约（lease_owner 以 host- 开头但不等于本实例ID）。
	tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Where("lease_owner LIKE ?", host+"-%").
		Where("lease_owner <> ?", GetInstanceId()).
		Updates(map[string]interface{}{
			"run_status":      RunStatusWait,
			"lease_owner":     "",
			"lease_expire_at": nil,
			"update_time":     now,
		})
	return tx.Error
}
