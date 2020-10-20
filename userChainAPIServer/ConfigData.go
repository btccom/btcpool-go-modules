package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"time"
)

// AutoRegAPIConfig 用户自动注册API定义
type AutoRegAPIConfig struct {
	IntervalSeconds time.Duration
	URL             string
	User            string
	Password        string
	DefaultCoin     string
	PostData        map[string]string
}

// ConfigData 配置数据
type ConfigData struct {
	// 挖矿服务器对子账户名大小写不敏感，此时将总是写入小写的子账户名
	StratumServerCaseInsensitive bool

	// Zookeeper集群的IP:端口列表
	ZKBroker []string
	// ZKSwitcherWatchDir Switcher监控的Zookeeper路径，以斜杠结尾
	ZKSwitcherWatchDir string

	// UserListAPI 币种对应的用户列表，形如{"btc":"url", "bcc":"url"}
	UserListAPI map[string]string
	// FetchUserListIntervalSeconds 每次拉取的间隔时间
	FetchUserListIntervalSeconds uint

	// 是否启用 API Server
	EnableAPIServer bool
	// API Server 的监听IP:端口
	ListenAddr string
	// API 用户名
	APIUser string
	// API 密码
	APIPassword string
	// AvailableCoins 可用币种，形如 {"btc", "bcc", ...}
	AvailableCoins []string

	// 定时检测间隔时间
	FetchUserCoinIntervalSeconds int
	// 用户:币种对应表的URL
	UserCoinMapURL string

	// EnableUserAutoReg 启用用户自动注册
	EnableUserAutoReg bool
	// ZKAutoRegWatchDir 用户自动注册的zookeeper监控地址，以斜杠结尾
	ZKAutoRegWatchDir string
	// UserAutoRegAPI 用户自动注册API
	UserAutoRegAPI AutoRegAPIConfig

	//子池更新用的zookeeper根目录（注意，不应包括币种和子池名称），以斜杠结尾
	ZKSubPoolUpdateBaseDir string
	// 子池更新时jobmaker的应答超时时间，如果在该时间内jobmaker没有应答，则API返回错误
	ZKSubPoolUpdateAckTimeout int
}

// ReadConfigFile 读取配置文件
func ReadConfigFile(configFilePath string) (configData *ConfigData, err error) {
	configJSON, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return
	}
	configData = new(ConfigData)
	err = json.Unmarshal(configJSON, configData)
	if err != nil {
		return
	}

	if len(configData.ZKSwitcherWatchDir) <= 0 {
		err = errors.New("Wrong config: ZKSwitcherWatchDir cannot be empty")
		return
	}
	// 若zookeeper路径不以“/”结尾，则添加
	if configData.ZKSwitcherWatchDir[len(configData.ZKSwitcherWatchDir)-1] != '/' {
		configData.ZKSwitcherWatchDir += "/"
	}

	if configData.EnableUserAutoReg {
		if len(configData.ZKAutoRegWatchDir) <= 0 {
			err = errors.New("Wrong config: UserAutoReg enabled, ZKAutoRegWatchDir cannot be empty")
			return
		}
		if configData.ZKAutoRegWatchDir[len(configData.ZKAutoRegWatchDir)-1] != '/' {
			configData.ZKAutoRegWatchDir += "/"
		}
	}

	if len(configData.ZKSubPoolUpdateBaseDir) > 0 && configData.ZKSubPoolUpdateBaseDir[len(configData.ZKSubPoolUpdateBaseDir)-1] != '/' {
		configData.ZKSubPoolUpdateBaseDir += "/"
	}

	// 设置默认值
	if configData.FetchUserListIntervalSeconds < 1 {
		configData.FetchUserListIntervalSeconds = 10
	}
	if configData.FetchUserCoinIntervalSeconds < 1 {
		configData.FetchUserCoinIntervalSeconds = 60
	}
	if configData.ZKSubPoolUpdateAckTimeout < 1 {
		configData.ZKSubPoolUpdateAckTimeout = 5
	}

	return
}