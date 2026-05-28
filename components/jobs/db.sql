create table job_record
(
    id              int auto_increment
        primary key,
    system_name     varchar(64)  default '' not null comment '系统名称',
    job_name        varchar(64)  default '' not null comment '任务名称',
    job_type        varchar(64)  default '' not null comment '任务类型[定时任务cron_job;定时webhook任务cron_hook]',
    cron_expr       varchar(32)  default '' not null comment 'cron表达式',
    service_name    varchar(128) default '' not null comment '服务[服务文件二进制path;CronServiceName]',
    service_method  varchar(64)  default '' not null comment '服务方法名称',
    args            longtext                null,
    job_result_flag varchar(32)  default '' not null comment '结果成功与否[success;failed]',
    hook_url        varchar(512) default '' not null comment 'webhook地址(仅供cron_webhook方式调用)',
    run_err         text                    null,
    run_result      text                    null,
    create_time     datetime(3)             null comment '创建时间',
    update_time     datetime(3)             null comment '更新时间',
    timeout         int                     null comment '超时时间',
    duration_ms     bigint       default 0  not null comment '执行耗时(毫秒)',
    instance_id     varchar(96)  default '' not null comment '执行该次任务的进程实例ID'
)
    collate = utf8mb3_bin;

;

create table job_center
(
    id                  int auto_increment
        primary key,
    system_name         varchar(64)  default ''       not null comment '系统名称',
    job_name            varchar(64)  default ''       not null comment '任务名称',
    job_type            varchar(64)  default ''       not null comment '任务类型[定时任务cron_job;定时webhook任务cron_hook]',
    cron_expr           varchar(32)  default ''       not null comment 'cron表达式',
    service_name        varchar(128) default ''       not null comment '服务[服务文件二进制path;CronServiceName]',
    service_method      varchar(64)  default ''       not null comment '服务方法名称',
    timeout             int                           null comment '超时时间',
    args                longtext                      null,
    hook_url            varchar(512) default ''       not null comment 'webhook地址(仅供cron_webhook方式调用)',
    run_status          varchar(32)  default '待运行' not null comment '运行状态[待运行;运行中]',
    last_run_time       datetime(3)                   null comment '最后一次运行时间',
    create_time         datetime(3)                   null comment '创建时间',
    update_time         datetime(3)                   null comment '更新时间',
    enabled             tinyint      default 0        not null comment '是否启用[0:关闭;1:启用]',
    lease_owner         varchar(96)  default ''       not null comment '当前租约持有者(实例ID),空=无主',
    lease_expire_at     datetime(3)                   null comment '租约到期时间;NULL或<NOW()可被任意实例抢占',
    last_heartbeat_time datetime(3)                   null comment '执行中实例的最近心跳时间',
    constraint job_center_pk
        unique (system_name, job_name)
)
    collate = utf8mb3_bin;

-- 已存在表的迁移语句（增量执行）：
-- ALTER TABLE job_center
--     ADD COLUMN system_name varchar(64) default '' not null,
--     ADD COLUMN lease_owner         varchar(96) default '' not null,
--     ADD COLUMN lease_expire_at     datetime(3)            null,
--     ADD COLUMN last_heartbeat_time datetime(3)            null;
-- ALTER TABLE job_record
--     ADD COLUMN system_name varchar(64) default '' not null,
--     ADD COLUMN instance_id varchar(96) default '' not null;
-- ALTER TABLE job_center
--     DROP INDEX job_center_pk,
--     ADD UNIQUE KEY job_center_pk (system_name, job_name);


