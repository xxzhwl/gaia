create table asynctasks
(
    id                bigint auto_increment
        primary key,
    system_name       varchar(32)  default ''                not null comment '系统名称',
    service_name      varchar(32)  default ''                not null comment '服务名称',
    method_name       varchar(64)  default ''                not null comment '方法名称',
    task_name         varchar(128) default ''                not null,
    arg               longtext                               null comment '参数',
    task_status       varchar(32)  default 'Wait'            not null,
    create_time       datetime     default CURRENT_TIMESTAMP not null,
    update_time       datetime     default CURRENT_TIMESTAMP not null on update CURRENT_TIMESTAMP,
    max_retry_time    int          default 0                 not null,
    retry_time        int          default 0                 not null,
    last_result       longtext                               null,
    last_err_msg      varchar(512) default ''                not null,
    last_run_time     datetime(3)                            null comment '最后一次运行时间',
    last_run_end_time datetime(3)                            null comment '最后一次运行结束时间',
    last_run_duration int          default 0                 not null comment '最后一次运行时长'
)
    comment '异步任务表' charset = utf8;

create index asynctasks_create_time_index
    on asynctasks (create_time);

create index asynctasks_method_name_index
    on asynctasks (method_name);

create index asynctasks_service_name_index
    on asynctasks (service_name);

create index asynctasks_system_name_index
    on asynctasks (system_name);

create index asynctasks_task_name_index
    on asynctasks (task_name);

create index asynctasks_task_status_index
    on asynctasks (task_status);



CREATE TABLE `async_task_heartbeat` (
                                        `id` int(11) NOT NULL AUTO_INCREMENT,
                                        `task_id` int(11) NOT NULL COMMENT '异步任务id',
                                        `heart_beat_time` datetime(3) DEFAULT NULL COMMENT '心跳时间',
                                        `heart_beat_nano_time` bigint(20) DEFAULT '0',
                                        PRIMARY KEY (`id`),
                                        UNIQUE KEY `async_task_heartbeat_task_id_uindex` (`task_id`)
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8 COLLATE=utf8_bin COMMENT='异步任务心跳表';

CREATE TABLE `asynctask_exec_row` (
                                      `id` bigint NOT NULL AUTO_INCREMENT,
                                      `task_id` bigint NOT NULL DEFAULT '0' COMMENT '任务id',
                                      `task_status` varchar(32) NOT NULL DEFAULT 'Wait',
                                      `last_result` longtext,
                                      `last_err_msg` varchar(512) NOT NULL DEFAULT '',
                                      `last_run_time` datetime(3) DEFAULT NULL COMMENT '最后一次运行时间',
                                      `last_run_end_time` datetime(3) DEFAULT NULL COMMENT '最后一次运行结束时间',
                                      `last_run_duration` int NOT NULL DEFAULT '0' COMMENT '最后一次运行时长',
                                      `log_id` varchar(128) NOT NULL DEFAULT '' COMMENT '日志id',
                                      PRIMARY KEY (`id`),
                                      KEY `asynctasks_create_time_index` (`last_run_time`)
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8 COMMENT='异步任务执行记录表'

