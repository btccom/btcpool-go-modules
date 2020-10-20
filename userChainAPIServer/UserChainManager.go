package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/samuel/go-zookeeper/zk"
)

// UserChainInfo 用户链信息
type UserChainInfo struct {
	userName    string           // internal field
	ChainName   string           `json:"chain"`
	SubPoolName string           `json:"subpool"`
	PUIDs       map[string]int32 `json:"puids"`
}

// NewUserChainInfo 创建UserChainInfo对象
func NewUserChainInfo(userName string) *UserChainInfo {
	userChainInfo := new(UserChainInfo)
	userChainInfo.userName = userName
	userChainInfo.PUIDs = make(map[string]int32)
	return userChainInfo
}

// UserChainManager 用户链信息管理器
type UserChainManager struct {
	configData   *ConfigData
	zookeeper    *Zookeeper
	mutex        *sync.RWMutex
	userChainMap map[string]*UserChainInfo

	lastPUID        map[string]int32 // 上次获取的最大PUID
	lastRequestDate int64            // 上次请求接口的时间
}

// UserIDMapResponse 用户id列表接口响应的数据结构
type UserIDMapResponse struct {
	ErrNo  int              `json:"err_no"`
	ErrMsg string           `json:"err_msg"`
	Data   map[string]int32 `json:"data"`
}

// UserIDMapEmptyResponse 用户id列表接口在用户数为0时候的响应
type UserIDMapEmptyResponse struct {
	ErrNo  int           `json:"err_no"`
	ErrMsg string        `json:"err_msg"`
	Data   []interface{} `json:"data"`
}

// UserCoinMapData 用户币种列表接口响应的data字段数据结构
type UserCoinMapData struct {
	UserCoin map[string]string `json:"user_coin"`
	NowDate  int64             `json:"now_date"`
}

// UserCoinMapResponse 用户币种列表接口响应的数据结构
type UserCoinMapResponse struct {
	ErrNo  int             `json:"err_no"`
	ErrMsg string          `json:"err_msg"`
	Data   UserCoinMapData `json:"data"`
}

// NewUserChainManager 初始化用户链信息管理器
func NewUserChainManager(configData *ConfigData, zookeeper *Zookeeper) *UserChainManager {
	manager := new(UserChainManager)
	manager.configData = configData
	manager.zookeeper = zookeeper
	manager.mutex = new(sync.RWMutex)
	manager.userChainMap = make(map[string]*UserChainInfo)
	manager.lastPUID = make(map[string]int32)
	return manager
}

// ReadFromZK 从ZK读取用户链信息
func (manager *UserChainManager) ReadFromZK(userName string) (info *UserChainInfo, err error) {
	zkPath := manager.configData.ZKSwitcherWatchDir + userName
	jsonBytes, _, err := manager.zookeeper.Get(zkPath)
	if err != nil {
		return
	}

	// map中存储的是指针，所以必须全程持有锁
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	info, ok := manager.userChainMap[userName]
	if !ok {
		info = NewUserChainInfo(userName)
		// map中存储的是指针，所以可以提前回填
		manager.userChainMap[userName] = info
	}

	// map中存储的是指针，所以可以直接修改，不需要回填
	err = json.Unmarshal(jsonBytes, info)
	if err == nil {
		glog.Info("ReadFromZK : ", info)
	}
	return
}

// WriteToZK 用户链信息写入ZK
func (manager *UserChainManager) WriteToZK(userName string) (err error) {
	// map中存储的是指针，所以必须全程持有锁
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()

	info, ok := manager.userChainMap[userName]
	if !ok {
		err = errors.New("User " + userName + " does not exists")
		return
	}
	jsonBytes, err := json.Marshal(info)
	if err != nil {
		return
	}
	zkPath := manager.configData.ZKSwitcherWatchDir + userName
	exists, stat, err := manager.zookeeper.Exists(zkPath)
	if err != nil {
		return
	}
	if exists {
		_, err = manager.zookeeper.Set(zkPath, jsonBytes, stat.Version)
	} else {
		_, err = manager.zookeeper.Create(zkPath, jsonBytes, 0, zk.WorldACL(zk.PermAll))
	}
	if err == nil {
		glog.Info("WriteToZK : ", info)
	}
	return
}

// FlushAllToZK 把所有用户币种信息写入ZK
func (manager *UserChainManager) FlushAllToZK() (err error) {
	// map中存储的是指针，所以必须全程持有锁
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()

	for user := range manager.userChainMap {
		err = manager.WriteToZK(user)
		if err != nil {
			return
		}
	}

	return
}

// GetChain 获取用户所挖币种
func (manager *UserChainManager) GetChain(userName string) string {
	// map中存储的是指针，所以必须全程持有锁
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	info, ok := manager.userChainMap[userName]
	if !ok {
		return ""
	}
	return info.ChainName
}

// RegularUserName 规范化传入的用户名
func (manager *UserChainManager) RegularUserName(userName string) string {
	if strings.Contains(userName, "_") {
		// remove chain postfix of puname
		userName = userName[0:strings.LastIndex(userName, "_")]
	}
	if manager.configData.StratumServerCaseInsensitive {
		userName = strings.ToLower(userName)
	}
	return userName
}

// SetPUID 设置用户在特定币种下的puid
func (manager *UserChainManager) SetPUID(userName string, chain string, puid int32) {
	// map中存储的是指针，所以必须全程持有锁
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	info, ok := manager.userChainMap[userName]
	if !ok {
		info = NewUserChainInfo(userName)
		// map中存储的是指针，所以可以提前回填
		manager.userChainMap[userName] = info
	}

	// map中存储的是指针，所以可以直接修改，不需要回填
	info.PUIDs[chain] = puid

	if len(info.ChainName) <= 0 {
		info.ChainName = chain
	}

	glog.Info("[SetPUID] ", userName, " (", chain, ") : ", puid)
}

// SetChain 设置用户所挖币种
func (manager *UserChainManager) SetChain(userName string, chain string) {
	// map中存储的是指针，所以必须全程持有锁
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	info, ok := manager.userChainMap[userName]
	if !ok {
		info = NewUserChainInfo(userName)
		// map中存储的是指针，所以可以提前回填
		manager.userChainMap[userName] = info
	}

	glog.Info("[SetChain] ", userName, " : ", info.ChainName, " -> ", chain)
	info.ChainName = chain
}

// SetSubPool 设置用户所在的子池
func (manager *UserChainManager) SetSubPool(userName string, subpool string) {
	// map中存储的是指针，所以必须全程持有锁
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	info, ok := manager.userChainMap[userName]
	if !ok {
		info = NewUserChainInfo(userName)
		// map中存储的是指针，所以可以提前回填
		manager.userChainMap[userName] = info
	}

	glog.Info("[SetSubPool] ", userName, " : ", info.SubPoolName, " -> ", subpool)
	info.SubPoolName = subpool
}

// FetchUserIDList 拉取用户id列表来更新用户puid/币种记录
func (manager *UserChainManager) FetchUserIDList(chain string, update bool) error {
	url := manager.configData.UserListAPI[chain]
	if lastPUID, ok := manager.lastPUID[chain]; ok {
		url += "?last_id=" + strconv.Itoa(int(lastPUID))
	} else {
		manager.lastPUID[chain] = 0
	}

	glog.Info("FetchUserIDList ", url)
	response, err := http.Get(url)

	if err != nil {
		return errors.New("HTTP Request Failed: " + err.Error())
	}

	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return errors.New("HTTP Fetch Body Failed: " + err.Error())
	}

	userIDMapResponse := new(UserIDMapResponse)
	err = json.Unmarshal(body, userIDMapResponse)

	if err != nil {
		// 用户id接口在返回0个用户的时候data字段数据类型会由object变成array，需要用另一个struct解析
		userIDMapEmptyResponse := new(UserIDMapEmptyResponse)
		err = json.Unmarshal(body, userIDMapEmptyResponse)

		if err != nil {
			return errors.New("Parse Result Failed: " + err.Error() + "; " + string(body))
		}

		glog.Info("Finish: ", chain, "; No New User", "; ", url)
		return nil
	}

	if userIDMapResponse.ErrNo != 0 {
		return errors.New("API Returned a Error: " + string(body))
	}

	glog.Info("HTTP GET Success. User Num: ", len(userIDMapResponse.Data))

	// 遍历用户币种列表
	for puname, puid := range userIDMapResponse.Data {
		puname = manager.RegularUserName(puname)
		manager.SetPUID(puname, chain, puid)
		if puid > manager.lastPUID[chain] {
			manager.lastPUID[chain] = puid
		}
		if update {
			err = manager.WriteToZK(puname)
			if err != nil {
				glog.Error("WriteToZK(", puname, ") failed: ", err)
			}
		}
	}

	glog.Info("Finish: ", chain, "; User Num: ", len(userIDMapResponse.Data), "; ", url)
	return nil
}

// FetchUserCoinMap 拉取用户币种映射表来更新用户币种记录
func (manager *UserChainManager) FetchUserCoinMap(update bool) error {
	url := manager.configData.UserCoinMapURL
	// 若上次请求过接口，则附加上次请求的时间到url
	if manager.lastRequestDate > 0 {
		// 减去configData.CronIntervalSeconds是为了防止出现竟态条件。
		// 比如在上次拉取之后，同一秒内又有币种切换，如果不减去，就可能会错过这个切换消息。
		url += "?last_date=" + strconv.FormatInt(manager.lastRequestDate-int64(manager.configData.FetchUserCoinIntervalSeconds), 10)
	}
	glog.Info("FetchUserCoinMap ", url)
	response, err := http.Get(url)

	if err != nil {
		return errors.New("HTTP Request Failed: " + err.Error())
	}

	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return errors.New("HTTP Fetch Body Failed: " + err.Error())
	}

	userCoinMapResponse := new(UserCoinMapResponse)

	err = json.Unmarshal(body, userCoinMapResponse)

	if err != nil {
		return errors.New("Parse Result Failed: " + err.Error() + "; " + string(body))
	}

	if userCoinMapResponse.ErrNo != 0 {
		return errors.New("API Returned a Error: " + string(body))
	}

	// 记录本次请求的时间
	manager.lastRequestDate = userCoinMapResponse.Data.NowDate

	glog.Info("HTTP GET Success. TimeStamp: ", userCoinMapResponse.Data.NowDate, "; UserCoin Num: ", len(userCoinMapResponse.Data.UserCoin))

	// 遍历用户币种列表
	for puname, chain := range userCoinMapResponse.Data.UserCoin {
		puname = manager.RegularUserName(puname)
		manager.SetChain(puname, chain)
		if update {
			err = manager.WriteToZK(puname)
			if err != nil {
				glog.Error("WriteToZK(", puname, ") failed: ", err)
			}
		}
	}
	return nil
}

// RunFetchUserIDListCronJob 运行定时拉取用户ID列表任务
func (manager *UserChainManager) RunFetchUserIDListCronJob(waitGroup *sync.WaitGroup, chain string) {
	defer waitGroup.Done()
	for {
		time.Sleep(time.Duration(manager.configData.FetchUserListIntervalSeconds) * time.Second)
		err := manager.FetchUserIDList(chain, true)
		if err != nil {
			glog.Error("FetchUserIDList(", chain, ") failed: ", err)
		}
	}
}

// RunFetchUserCoinMapCronJob 运行定时拉取用户币种映射表任务
func (manager *UserChainManager) RunFetchUserCoinMapCronJob(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	for {
		time.Sleep(time.Duration(manager.configData.FetchUserCoinIntervalSeconds) * time.Second)
		err := manager.FetchUserCoinMap(true)
		if err != nil {
			glog.Error("FetchUserCoinMap() failed: ", err)
		}
	}
}