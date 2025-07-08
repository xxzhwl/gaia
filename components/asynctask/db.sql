CREATE TABLE `asynctasks` (
                              `id` bigint(20) NOT NULL AUTO_INCREMENT,
                              `system_name` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '系统名称',
                              `service_name` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '服务名称',
                              `method_name` varchar(64) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '方法名称',
                              `task_name` varchar(128) COLLATE utf8_bin NOT NULL DEFAULT '',
                              `arg` longtext COLLATE utf8_bin COMMENT '参数',
                              `task_status` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT 'Wait',
                              `create_time` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
                              `update_time` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
                              `max_retry_time` int(11) NOT NULL DEFAULT '0',
                              `retry_time` int(11) NOT NULL DEFAULT '0',
                              `last_result` longtext COLLATE utf8_bin,
                              `last_err_msg` varchar(512) COLLATE utf8_bin NOT NULL DEFAULT '',
                              `last_run_time` datetime(3) DEFAULT NULL COMMENT '最后一次运行时间',
                              `last_run_end_time` datetime(3) DEFAULT NULL COMMENT '最后一次运行结束时间',
                              `last_run_duration` int(11) NOT NULL DEFAULT '0' COMMENT '最后一次运行时长',
                              PRIMARY KEY (`id`),
                              KEY `asynctasks_create_time_index` (`create_time`),
                              KEY `asynctasks_method_name_index` (`method_name`),
                              KEY `asynctasks_service_name_index` (`service_name`),
                              KEY `asynctasks_system_name_index` (`system_name`),
                              KEY `asynctasks_task_name_index` (`task_name`),
                              KEY `asynctasks_task_status_index` (`task_status`)
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8 COLLATE=utf8_bin COMMENT='异步任务表';

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
                                      PRIMARY KEY (`id`),
                                      KEY `asynctasks_create_time_index` (`last_run_time`)
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8 COMMENT='异步任务执行记录表';