CREATE TABLE `sys_auth` (
                            `id` int(11) NOT NULL AUTO_INCREMENT,
                            `sys_id` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '',
                            `sys_key` varchar(512) COLLATE utf8_bin NOT NULL DEFAULT '',
                            `enable` tinyint(4) NOT NULL DEFAULT '0',
                            `duty` varchar(32) COLLATE utf8_bin NOT NULL DEFAULT '',
                            `create_time` datetime DEFAULT NULL,
                            `update_time` datetime DEFAULT NULL,
                            PRIMARY KEY (`id`),
                            KEY `sys_auth_sys_id_index` (`sys_id`)
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8 COLLATE=utf8_bin;