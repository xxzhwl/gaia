create table t_user
(
    id                 bigint auto_increment comment '用户id'
        primary key,
    user_name          varchar(32)  default ''      not null comment '用户名',
    password           varchar(512) default ''      not null comment '密码',
    mail               varchar(128) default ''      not null comment '邮箱',
    phone_region_num   int          default 86      not null comment '手机区号',
    phone_num          varchar(128) default ''      not null comment '手机号',
    create_time        datetime     default (now()) null comment '创建时间',
    update_time        datetime     default (now()) null comment '更新时间',
    log_out_time       datetime                     null comment '注销时间',
    is_ban             tinyint(1)   default 0       not null comment '是否被禁用[0:暂未禁用;1:已禁用]',
    is_log_out         tinyint      default 0       not null comment '是否注销[0:未注销;1:已注销]',
    create_time_stamp  bigint       default 0       not null comment '创建时间时间戳',
    update_time_stamp  bigint       default 0       not null comment '更新时间时间戳',
    log_out_time_stamp bigint       default 0       not null comment '注销时间戳'
)
    comment '用户表' collate = utf8_bin;

create index t_user_is_log_out_index
    on t_user (is_log_out);

create index t_user_mail_index
    on t_user (mail);

create index t_user_phone_region_num_phone_num_index
    on t_user (phone_region_num, phone_num);

create index t_user_user_name_index
    on t_user (user_name);

