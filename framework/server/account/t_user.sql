CREATE TABLE `t_user` (
                          `id` int NOT NULL AUTO_INCREMENT,
                          `user_name` varchar(32) COLLATE utf8mb3_bin NOT NULL DEFAULT '',
                          `password` varchar(512) COLLATE utf8mb3_bin NOT NULL DEFAULT '',
                          `mail` varchar(128) COLLATE utf8mb3_bin NOT NULL DEFAULT '',
                          `phone_region_num` int NOT NULL DEFAULT '86',
                          `phone_num` varchar(128) COLLATE utf8mb3_bin NOT NULL DEFAULT '',
                          `create_time` datetime DEFAULT NULL COMMENT '创建时间',
                          `update_time` datetime DEFAULT NULL,
                          `is_ban` tinyint(1) NOT NULL DEFAULT '0',
                          `is_log_out` tinyint(1) NOT NULL DEFAULT '0',
                          PRIMARY KEY (`id`),
                          UNIQUE KEY `t_user_pk` (`uid`),
                          KEY `t_user_mail_index` (`mail`),
                          KEY `t_user_phone_region_num_phone_num_index` (`phone_region_num`,`phone_num`),
                          KEY `t_user_user_name_index` (`user_name`),
                          KEY `t_user_uid_index` (`uid`)
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin

