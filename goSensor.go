package main

import (
	"net/http"
	"log"
	"github.com/go-redis/redis"
	"io"
	"strconv"
	"sync"
	"fmt"
	"time"
	"io/ioutil"
	"encoding/json"
	"os/exec"
	"regexp"
	"runtime"
	"bytes"
	"golang.org/x/crypto/ssh"
	"net"
)

var once sync.Once

const UploadKeyPrefix = "sensor_upload_key_"
const RedisDataKeyPrefix = "go_sensor_data_key_"
const PointInterval = 60 * 10
const DaysRange = 7

var redisInstance *redis.Client
var cpuNum = runtime.NumCPU()
//singleton
func Redis() *redis.Client {
	once.Do(func() {
		redisInstance = redis.NewClient(&redis.Options{Addr: "10.0.0.2:6379", Password: "", DB: 10,})
	})
	return redisInstance
}

func main() {
	//sensorsLoop()
	sensorJson()
	return
	http.HandleFunc("/loop", func(writer http.ResponseWriter, request *http.Request) {
		go sensorsLoop()
		io.WriteString(writer, "ok")
	})
	http.HandleFunc("/sensor/upload", sensorUpload)
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		start := time.Now()

		Redis := Redis()
		io.WriteString(writer, strconv.FormatInt(Redis.Incr("fdsfsdgfdwer").Val(), 10))
		fmt.Println(time.Since(start), request.URL)
	})

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err.Error())
	}
}

func sensorJson() {
	temperatureData := map[string]interface{}{
		"temperature_one": map[string]interface{}{
			"name":           "temperature_one",
			"redis_key":      RedisDataKeyPrefix + "one",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "temperature",
			"color":          "#FF9933",
			"order":          1000,
			"temperature":    []float64{},
		},
		"humidity_one": map[string]interface{}{
			"name":           "humidity_one",
			"redis_key":      RedisDataKeyPrefix + "one",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "humidity",
			"color":          "#0099ff",
			"order":          2000,
			"temperature":    []float64{},
		},
		"nas": map[string]interface{}{
			"name":           "nas",
			"redis_key":      RedisDataKeyPrefix + "nas",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "CPU",
			"color":          "#FF9933",
			"order":          3000,
			"temperature":    []float64{},
		},
		"pi": map[string]interface{}{
			"name":           "pi",
			"redis_key":      RedisDataKeyPrefix + "pi",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "CPU",
			"color":          "#FF9933",
			"order":          4000,
			"temperature":    []float64{},
		},
		"route": map[string]interface{}{
			"name":           "route",
			"redis_key":      RedisDataKeyPrefix + "route",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "CPU",
			"color":          "#FF9933",
			"order":          5000,
			"temperature":    []float64{},
		},
		"temperature_two": map[string]interface{}{
			"name":           "temperature_two",
			"redis_key":      RedisDataKeyPrefix + "two",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "temperature",
			"color":          "#FF9933",
			"order":          6000,
			"temperature":    []float64{},
		},
		"humidity_two": map[string]interface{}{
			"name":           "humidity_two",
			"redis_key":      RedisDataKeyPrefix + "two",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "humidity",
			"color":          "#0099ff",
			"order":          7000,
			"temperature":    []float64{},
		},
		"temperature_three": map[string]interface{}{
			"name":           "temperature_three",
			"redis_key":      RedisDataKeyPrefix + "three",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "temperature",
			"color":          "#FF9933",
			"order":          8000,
			"temperature":    []float64{},
		},
		"humidity_three": map[string]interface{}{
			"name":           "humidity_three",
			"redis_key":      RedisDataKeyPrefix + "three",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "humidity",
			"color":          "#0099ff",
			"order":          9000,
			"temperature":    []float64{},
		},
	}
	//fmt.Println(temperatureData)

	//startTime := int(time.Now().Unix())
	//lastAddTime := 0

	for _, tempValue := range temperatureData {
		item, ok := tempValue.(map[string]interface{})
		if !ok {
			continue
		}

		redisKey, ok := item["redis_key"].(string)
		if !ok {
			continue
		}
		list, _ := Redis().LRange(redisKey, 0, DaysRange*86400/PointInterval).Result()
		//fmt.Println(list)

		var jsonO = make(map[string]interface{})
		for _, jsonStr := range list {
			json.Unmarshal([]byte(jsonStr), &jsonO)

			item, ok := tempValue.(map[string]interface{})
			if !ok {
				continue
			}

			pointStart := item["point_start"]
			if pointStart == 0 {
				if _, ok := item["point_start"].(int); ok {
					jsonAddTime,_ :=jsonO["add_time"].(int64)
					item["point_start"] = jsonAddTime
					//lastAddTime = int(jsonAddTime)
					//temperature_data[device][data_key].append(float(v[data_key]))
				}
			}

		}
		//
		//item, ok := tempValue.(map[string]interface{})
		//if !ok {
		//	continue
		//}

		//pointStart := item["point_start"]
		//if pointStart == 0 {
		//	if _, ok := item["point_start"].(int); ok {
		//		item["point_start"] = item[""]
		//	}
		//}

		//if not temperature_data[device]['point_start']:
		//temperature_data[device]['point_start'] = v['add_time']
		//last_add_time = v['add_time']
		//temperature_data[device][data_key].append(float(v[data_key]))

	}

	fmt.Println(temperatureData)
	//
	//fmt.Println(list)
}

//同时只执行一次
var onceLock = false

func sensorsLoop() {
	//同时只执行一次
	if onceLock {
		return
	}
	onceLock = true

	start := time.Now()
	if res, ok := oneSensor(); ok {
		saveData("one", res)
	}
	fmt.Println(time.Since(start))
	start = time.Now()
	if res, ok := twoSensor(); ok {
		saveData("two", res)
	}
	fmt.Println(time.Since(start))
	start = time.Now()
	if res, ok := threeSensor(); ok {
		saveData("three", res)
	}
	fmt.Println(time.Since(start))
	start = time.Now()
	if res, ok := nasSensor(); ok {
		saveData("nas", res)
	}
	fmt.Println(time.Since(start))
	start = time.Now()
	if res, ok := routeSensor(); ok {
		saveData("route", res)
	}
	fmt.Println(time.Since(start))
	onceLock = false //同时只执行一次
}

func saveData(name string, data interface{}) {
	saveData := make(map[string]interface{})
	k, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	saveData = k
	if _, ok := saveData["name"]; !ok {
		saveData["name"] = name
	}
	if _, ok := saveData["add_time"]; !ok {
		saveData["add_time"] = time.Now().Unix()
	}

	saveStr, err := json.Marshal(saveData)
	if err != nil {
		log.Fatal(err)
	}
	Redis().RPush(RedisDataKeyPrefix+name, string(saveStr))

	fmt.Println(saveData)
}

func routeSensor() (map[string]interface{}, bool) {
	b, err := ioutil.ReadFile("/root/.ssh/route.600.key") // just pass the file name
	if err != nil {
		log.Fatal(err)
	}

	res, err := remoteRun("admin", "10.0.0.1", b, "cat /proc/dmu/temperature")
	if err != nil {
		log.Fatal(err)
	}

	re := regexp.MustCompile(`CPU\stemperature\s:\s(\d+\.?\d*)`)

	data := make(map[string]interface{}, 1)
	for _, v := range re.FindAllStringSubmatch(string(res), 1) {
		temp, err := strconv.ParseFloat(v[1], 64)
		if err != nil {
			log.Fatal(err)
		}
		data["CPU"] = temp
	}

	return data, true
}

//e.g. output, err := remoteRun("root", "MY_IP", "PRIVATE_KEY", "ls")
func remoteRun(user string, addr string, privateKey []byte, cmd string) (string, error) {
	// privateKey could be read from a file, or retrieved from another storage
	// source, such as the Secret Service / GNOME Keyring
	key, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	// Authentication
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		//alternatively, you could use a password
		/*
			Auth: []ssh.AuthMethod{
				ssh.Password("PASSWORD"),
			},
		*/
	}
	// Connect
	client, err := ssh.Dial("tcp", addr+":22", config)
	if err != nil {
		return "", err
	}
	// Create a session. It is one session per command.
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var b bytes.Buffer  // import "bytes"
	session.Stdout = &b // get output
	// you can also pass what gets input to the stdin, allowing you to pipe
	// content from client to server
	//      session.Stdin = bytes.NewBufferString("My input")

	// Finally, run the command
	err = session.Run(cmd)
	return b.String(), err
}

func nasSensor() (map[string]interface{}, bool) {
	// 执行系统命令
	// 第一个参数是命令名称
	// 后面参数可以有多个，命令参数
	cmd := exec.Command("sensors")
	// 获取输出对象，可以从该对象中读取输出结果
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	// 保证关闭输出流
	defer stdout.Close()
	// 运行命令
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	// 读取输出结果
	opBytes, err := ioutil.ReadAll(stdout)
	if err != nil {
		log.Fatal(err)
	}
	//log.Println(string(opBytes))

	re := regexp.MustCompile(`Core\s\d:\s+\+(\d+\.?\d*)`)

	data := make(map[string]interface{}, cpuNum)
	for k, v := range re.FindAllStringSubmatch(string(opBytes), cpuNum) {
		temp, err := strconv.ParseFloat(v[1], 64)
		if err != nil {
			log.Fatal(err)
		}

		data["CPU"+strconv.Itoa(k)] = temp
	}

	return data, true
}

func oneSensor() (map[string]interface{}, bool) {
	chip := "one"
	str, err := Redis().Get(UploadKeyPrefix + chip).Bytes()
	if err != nil {
		fmt.Println(chip + " 无数据")
		return make(map[string]interface{}), false
	}

	jsonO := make(map[string]interface{})
	json.Unmarshal(str, &jsonO)

	return jsonO, true
}

func twoSensor() (map[string]interface{}, bool) {
	chip := "two"
	str, err := Redis().Get(UploadKeyPrefix + chip).Bytes()
	if err != nil {
		fmt.Println(chip + " 无数据")
		return make(map[string]interface{}), false
	}

	jsonO := make(map[string]interface{})
	json.Unmarshal(str, &jsonO)

	return jsonO, true
}

func threeSensor() (map[string]interface{}, bool) {
	chip := "three"
	str, err := Redis().Get(UploadKeyPrefix + chip).Bytes()
	if err != nil {
		fmt.Println(chip + " 无数据")
		return make(map[string]interface{}), false
	}

	jsonO := make(map[string]interface{})
	json.Unmarshal(str, &jsonO)

	return jsonO, true
}

func sensorUpload(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Read body
	b, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var jsonObj interface{}

	err = json.Unmarshal(b, &jsonObj)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	data, ok := jsonObj.(map[string]interface{})
	if !ok {
		http.Error(w, "无法解析json", 500)
		return
	}

	if _, ok = data["add_time"]; !ok {
		data["add_time"] = time.Now().Unix()
	}

	if _, ok = data["chip"]; !ok {
		data["chip"] = "undefined"
	}

	insertStr, err := json.Marshal(data)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	Redis().Set(UploadKeyPrefix+data["chip"].(string), insertStr, 0)

	//fmt.Println(Redis().Get(UploadKeyPrefix+data["chip"].(string)))
	//
	//for k, v := range data {
	//	switch v2 := v.(type) {
	//	case string:
	//		fmt.Println(k, v2)
	//	case int:
	//		fmt.Println(k, v2)
	//	case int64:
	//		fmt.Println(k, v2)
	//	case bool:
	//		fmt.Println(k, v2)
	//	case float64:
	//		fmt.Println(k, v2)
	//	default:
	//		fmt.Println("无法解析的类型")
	//		fmt.Println(reflect.TypeOf(v))
	//		fmt.Println(k, v)
	//	}
	//}

	w.Header().Set("content-type", "application/json")
	w.Write(insertStr)
	//io.WriteString(w, "ok")
	fmt.Println(time.Since(start), r.URL)
}
