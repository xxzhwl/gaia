create table t_user_token
(
    id                 bigint auto_increment comment '主键id'
        primary key,
    user_id            bigint       default 0 not null comment '用户id',
    refresh_token      varchar(512)           not null comment '刷新令牌(加密存储)',
    device_type        varchar(32)  default '' not null comment '设备类型[web:网页端;ios:iOS;android:安卓;小程序:mini_program]',
    device_id          varchar(128) default '' not null comment '设备唯一标识',
    device_name        varchar(128) default '' not null comment '设备名称(如:iPhone 14 Pro)',
    os_version         varchar(64)  default '' not null comment '操作系统版本',
    app_version        varchar(32)  default '' not null comment '应用版本号',
    ip_address         varchar(64)  default '' not null comment '登录IP地址',
    location           varchar(128) default '' not null comment '登录地理位置',
    user_agent         varchar(512) default '' not null comment '用户代理信息',
    is_active          tinyint(1)   default 1  not null comment '是否活跃[0:已失效/已退出;1:活跃]',
    expired_time       datetime                not null comment '过期时间',
    last_active_time   datetime                not null comment '最后活跃时间',
    create_time        datetime     default (now()) not null comment '创建时间',
    update_time        datetime     default (now()) not null comment '更新时间',
    create_time_stamp  bigint       default 0     not null comment '创建时间戳',
    update_time_stamp  bigint       default 0     not null comment '更新时间戳',
    constraint fk_user_token_user_id foreign key (user_id) references t_user (id) on delete cascade
)
    comment '用户令牌表-支持多设备登录' collate = utf8_bin;

create index idx_user_token_user_id on t_user_token (user_id);
create index idx_user_token_refresh_token on t_user_token (refresh_token);
create index idx_user_token_device_id on t_user_token (device_id);
create index idx_user_token_is_active on t_user_token (is_active);
create index idx_user_token_expired_time on t_user_token (expired_time);
