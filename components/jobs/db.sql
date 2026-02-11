CREATE TABLE `job_record` (
                              `id` int(11) NOT NULL AUTO_INCREMENT,
                              `job_name` varchar(64) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '任务名称',
                              `job_type` varchar(64) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '任务类型[定时任务cron_job;定时webhook任务cron_hook]',
                              `cron_expr` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT 'cron表达式',
                              `service_name` varchar(128) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '服务[服务文件二进制path;CronServiceName]',
                              `service_method` varchar(64) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '服务方法名称',
                              `args` longtext COLLATE utf8_bin,
                              `job_result_flag` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '结果成功与否[success;failed]',
                              `hook_url` varchar(512) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT 'webhook地址(仅供cron_webhook方式调用)',
                              `run_err` text COLLATE utf8_bin,
                              `run_result` text COLLATE utf8_bin,
                              `create_time` datetime(3) DEFAULT NULL COMMENT '创建时间',
                              `update_time` datetime(3) DEFAULT NULL COMMENT '更新时间',
                              PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_bin;

CREATE TABLE `job_center` (
                              `id` int(11) NOT NULL AUTO_INCREMENT,
                              `job_name` varchar(64) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '任务名称',
                              `job_type` varchar(64) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '任务类型[定时任务cron_job;定时webhook任务cron_hook]',
                              `cron_expr` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT 'cron表达式',
                              `service_name` varchar(128) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '服务[服务文件二进制path;CronServiceName]',
                              `service_method` varchar(64) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT '服务方法名称',
                              `args` longtext COLLATE utf8_bin,
                              `hook_url` varchar(512) COLLATE utf8_bin NOT NULL DEFAULT '' COMMENT 'webhook地址(仅供cron_webhook方式调用)',
                              `run_status` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '待运行' COMMENT '运行状态[待运行;运行中]',
                              `last_run_time` datetime(3) DEFAULT NULL COMMENT '最后一次运行时间',
                              `create_time` datetime(3) DEFAULT NULL COMMENT '创建时间',
                              `update_time` datetime(3) DEFAULT NULL COMMENT '更新时间',
                              `enabled` tinyint(4) NOT NULL DEFAULT '0' COMMENT '是否启用[0:关闭;1:启用]',
                              PRIMARY KEY (`id`),
                              UNIQUE KEY `job_center_pk` (`job_name`)
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8 COLLATE=utf8_bin;



