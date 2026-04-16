create table job_record
(
    id              int auto_increment
        primary key,
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
    timeout         int                     null comment '超时时间'
)
    collate = utf8mb3_bin;

;

create table job_center
(
    id             int auto_increment
        primary key,
    job_name       varchar(64)  default ''       not null comment '任务名称',
    job_type       varchar(64)  default ''       not null comment '任务类型[定时任务cron_job;定时webhook任务cron_hook]',
    cron_expr      varchar(32)  default ''       not null comment 'cron表达式',
    service_name   varchar(128) default ''       not null comment '服务[服务文件二进制path;CronServiceName]',
    service_method varchar(64)  default ''       not null comment '服务方法名称',
    timeout        int                           null comment '超时时间',
    args           longtext                      null,
    hook_url       varchar(512) default ''       not null comment 'webhook地址(仅供cron_webhook方式调用)',
    run_status     varchar(32)  default '待运行' not null comment '运行状态[待运行;运行中]',
    last_run_time  datetime(3)                   null comment '最后一次运行时间',
    create_time    datetime(3)                   null comment '创建时间',
    update_time    datetime(3)                   null comment '更新时间',
    enabled        tinyint      default 0        not null comment '是否启用[0:关闭;1:启用]',
    constraint job_center_pk
        unique (job_name)
)
    collate = utf8mb3_bin;



