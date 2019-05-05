package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis"
	"golang.org/x/crypto/ssh"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var once sync.Once

const UploadKeyPrefix = "sensor_upload_key_"
const RedisDataKeyPrefix = "go_sensor_data_key_"
const RedisSensorJsonKey = "sensor_json_cache_key_1"
const PointInterval = 60 * 10
const DaysRange = 7

var redisInstance *redis.Client
var cpuNum = runtime.NumCPU()

//singleton
func Redis() *redis.Client {
	once.Do(func() {
		redisInstance = redis.NewClient(&redis.Options{Addr: "10.0.0.2:6379", Password: "", DB: 10})
	})
	return redisInstance
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func commonHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			fn(w, r)
			return
		}
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Server", "flySay.com")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		gzr := gzipResponseWriter{Writer: gz, ResponseWriter: w}
		fn(gzr, r)
	}
}

func main() {
	//logging
	defer func() {
		if r := recover(); r != nil {
			logfile, err := os.OpenFile("goSensor.error.log", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0)
			if err != nil {
				fmt.Printf("%s\r\n", err.Error())
				os.Exit(-1)
			}
			defer logfile.Close()
			logger := log.New(logfile, "\r\n", log.Ldate|log.Ltime|log.Llongfile)
			logger.Println(r)
		}
	}()

	http.HandleFunc("/nocache/sensor.json", commonHandler(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		res := sensorJsonCache()

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, res)
		fmt.Println(time.Since(start), r.URL)
	}))

	http.HandleFunc("/sensor.json", commonHandler(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		res, err := Redis().Get(RedisSensorJsonKey).Result()
		if err != nil {
			res = sensorJsonCache()
		}

		if limit, ok := r.URL.Query()["limit"]; ok && len(limit) == 1 {
			if count, err := strconv.Atoi(limit[0]); err == nil && count >= 0 {
				jsonData := make([]map[string]interface{}, 1)
				json.Unmarshal([]byte(res), &jsonData)
				for index, item := range jsonData {
					temp := jsonData[index][item["index"].(string)].([]interface{})
					tempCount := len(temp)
					if tempCount > count {
						jsonData[index][item["index"].(string)] = temp[tempCount-count:]
					}
				}

				if byteStr, err := json.Marshal(jsonData); err == nil {
					res = string(byteStr)
				}
			}
		}
		//fmt.Println(len(jsonData))

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, res)
		fmt.Println(time.Since(start), r.URL)
	}))

	http.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		sensorsLoop()
		sensorJsonCache()
		io.WriteString(w, "ok")
	})

	http.HandleFunc("/nas.json", commonHandler(func(w http.ResponseWriter, r *http.Request) {
		str, ok := nasSensor()
		if !ok {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(500)
			io.WriteString(w, "Failed to read sensors")
			return
		}
		byteStr, _ := json.Marshal(str)
		w.Header().Set("Content-Type", "application/json")
		w.Write(byteStr)
	}))

	http.HandleFunc("/sensor/upload", sensorUpload)

	http.HandleFunc("/static/js/jquery-2.1.1.min.js", commonHandler(func(w http.ResponseWriter, r *http.Request) {
		//prefix := "/static"
		//file := prefix + r.URL.Path[len(prefix)-1:]
		file := "static/js/jquery-2.1.1.min.js"
		http.ServeFile(w, r, file)
	}))

	http.HandleFunc("/static/js/highcharts.js", commonHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-javascript")
		//prefix := "/static"
		//file := prefix + r.URL.Path[len(prefix)-1:]
		file := "static/js/highcharts.js"
		http.ServeFile(w, r, file)
	}))

	http.HandleFunc("/", commonHandler(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		w.Header().Set("Content-Type", "text/html")
		if string(r.URL.Path) != "/" {
			w.WriteHeader(404)
			return
		}

		html, err := template.ParseFiles("template/index.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		html.Execute(w, nil)

		fmt.Println(time.Since(start), r.URL)
	}))

	err := http.ListenAndServe(":88", nil)
	if err != nil {
		fmt.Println(err)
	}
}

func sensorJsonCache() string {
	byteStr, _ := sensorJson()
	Redis().Set(RedisSensorJsonKey, string(byteStr), 800e9) //800s
	return string(byteStr)
}

func sensorJson() ([]byte, error) {
	var temperatureData = map[string]map[string]interface{}{
		"nas": {
			"name":           "nas",
			"redis_key":      RedisDataKeyPrefix + "nas",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "CPU",
			"color":          "#FF9933",
			"order":          1000,
			"unit":           "Degrees",
			"CPU":            []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},
		"pi": {
			"name":           "pi",
			"redis_key":      RedisDataKeyPrefix + "pi",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "CPU",
			"color":          "#FF9933",
			"order":          2000,
			"unit":           "Degrees",
			"CPU":            []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},
		"route": {
			"name":           "route",
			"redis_key":      RedisDataKeyPrefix + "route",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "CPU",
			"color":          "#FF9933",
			"order":          3000,
			"unit":           "Degrees",
			"CPU":            []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},

		//		"temperature_one": {
		//			"name":           "room_temperature",
		//			"redis_key":      RedisDataKeyPrefix + "one",
		//			"point_start":    0,
		//			"point_interval": PointInterval,
		//			"index":          "temperature",
		//			"color":          "#FF9933",
		//			"order":          4000,
		//			"unit":           "Degrees",
		//			"temperature":    []interface{}{},
		//			"max":            -9999.0,
		//			"min":            99999.0,
		//			"max_time":       0,
		//			"min_time":       0,
		//		},
		//		"humidity_one": {
		//			"name":           "room_humidity",
		//			"redis_key":      RedisDataKeyPrefix + "one",
		//			"point_start":    0,
		//			"point_interval": PointInterval,
		//			"index":          "humidity",
		//			"color":          "#0099ff",
		//			"order":          5000,
		//			"unit":           "Percent",
		//			"humidity":       []interface{}{},
		//			"max":            -9999.0,
		//			"min":            99999.0,
		//			"max_time":       0,
		//			"min_time":       0,
		//		},
		"temperature_two": {
			"name":           "bedroom_temperature",
			"redis_key":      RedisDataKeyPrefix + "two",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "temperature",
			"color":          "#FF9933",
			"unit":           "Degrees",
			"order":          6000,
			"temperature":    []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},
		"humidity_two": {
			"name":           "bedroom_humidity",
			"redis_key":      RedisDataKeyPrefix + "two",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "humidity",
			"color":          "#0099ff",
			"order":          7000,
			"unit":           "Percent",
			"humidity":       []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},
		"temperature_three": {
			"name":           "outdoor_temperature",
			"redis_key":      RedisDataKeyPrefix + "three",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "temperature",
			"color":          "#FF9933",
			"order":          8000,
			"unit":           "Degrees",
			"temperature":    []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},
		//		"humidity_three": {
		//			"name":           "outdoor_humidity",
		//			"redis_key":      RedisDataKeyPrefix + "three",
		//			"point_start":    0,
		//			"point_interval": PointInterval,
		//			"index":          "humidity",
		//			"color":          "#0099ff",
		//			"order":          9000,
		//			"unit":           "Percent",
		//			"humidity":       []interface{}{},
		//			"max":            -9999.0,
		//			"min":            99999.0,
		//			"max_time":       0,
		//			"min_time":       0,
		//		},
		"temperature_four": {
			"name":           "portable_temperature",
			"redis_key":      RedisDataKeyPrefix + "four",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "temperature",
			"color":          "#FF9933",
			"order":          10000,
			"unit":           "Degrees",
			"temperature":    []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},
		"humidity_four": {
			"name":           "portable_humidity",
			"redis_key":      RedisDataKeyPrefix + "four",
			"point_start":    0,
			"point_interval": PointInterval,
			"index":          "humidity",
			"color":          "#0099ff",
			"order":          11000,
			"unit":           "Percent",
			"humidity":       []interface{}{},
			"max":            -9999.0,
			"min":            99999.0,
			"max_time":       0,
			"min_time":       0,
		},
	}
	lastAddTime := 0
	for _, tempValue := range temperatureData {
		item := tempValue
		//if !ok {
		//	continue
		//}

		redisKey, ok := item["redis_key"].(string)
		if !ok {
			continue
		}
		list, _ := Redis().LRange(redisKey, -DaysRange*86400/PointInterval, -1).Result()
		//fmt.Println(list)

		var jsonO = make(map[string]interface{})
		for _, jsonStr := range list {
			json.Unmarshal([]byte(jsonStr), &jsonO)

			if !ok {
				continue
			}
			jsonAddTime, _ := jsonO["add_time"].(float64)
			index, _ := item["index"].(string)
			indexValue, _ := jsonO[index].(float64)

			//max
			maxValue, _ := item["max"].(float64)
			if indexValue > maxValue {
				item["max"] = indexValue
				item["max_time"] = jsonAddTime
			}

			//min
			minValue, _ := item["min"].(float64)
			if indexValue < minValue {
				item["min"] = indexValue
				item["min_time"] = jsonAddTime
			}

			pointStart := item["point_start"]
			if pointStart == 0 {
				if _, ok := item["point_start"].(int); ok {
					item["point_start"] = jsonAddTime
					lastAddTime = int(jsonAddTime)
				}

				indexArr, _ := item[index].([]interface{})
				item[index] = append(indexArr[:], indexValue)
			}

			whileFlag := 0
			for {
				if lastAddTime >= int(jsonAddTime) {
					break
				}

				if whileFlag > 1 {
					indexArr, _ := item[index].([]interface{})
					item[index] = append(indexArr[:], nil)
				} else {
					indexArr, _ := item[index].([]interface{})
					item[index] = append(indexArr[:], indexValue)
				}
				lastAddTime += PointInterval
				whileFlag += 1
			}
		}
	}

	//delete item if empty
	for k, item := range temperatureData {
		//item, _ := v.(map[string]interface{})
		index, _ := item["index"].(string)
		itemArr, _ := item[index].([]interface{})
		if len(itemArr) == 0 {
			delete(temperatureData, k)
		}
	}

	//map to  slice
	var sortData []map[string]interface{}
	for _, value := range temperatureData {
		//if value, ok := v.(map[string]interface{}); ok {
		sortData = append(sortData, value)
		//}
	}

	//sorted by order
	sort.Slice(sortData, func(i, j int) bool {
		first, _ := sortData[i]["order"].(int)
		second, _ := sortData[j]["order"].(int)
		return first < second
	})

	jsonStr, err := json.Marshal(sortData)
	return jsonStr, err
	//
	//fmt.Println(temperatureData)
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
	if res, ok := dhtSensor("one"); ok {
		saveData("one", res)
	}
	fmt.Println(time.Since(start))
	start = time.Now()
	if res, ok := dhtSensor("two"); ok {
		saveData("two", res)
	}
	fmt.Println(time.Since(start))
	start = time.Now()
	if res, ok := dhtSensor("three"); ok {
		saveData("three", res)
	}
	fmt.Println(time.Since(start))
	start = time.Now()
	if res, ok := dhtSensor("four"); ok {
		saveData("four", res)
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
		fmt.Println(err)
		return
	}
	Redis().RPush(RedisDataKeyPrefix+name, string(saveStr))

	fmt.Println(saveData)
}

func routeSensor() (map[string]interface{}, bool) {
	b, err := ioutil.ReadFile("/root/.ssh/route.600.key") // just pass the file name
	if err != nil {
		fmt.Println(err)
		return make(map[string]interface{}), false
	}

	res, err := remoteRun("admin", "10.0.0.1", b, "cat /proc/dmu/temperature")
	if err != nil {
		fmt.Println(err)
		return make(map[string]interface{}), false
	}

	re := regexp.MustCompile(`CPU\stemperature\s:\s(\d+\.?\d*)`)

	data := make(map[string]interface{}, 1)
	for _, v := range re.FindAllStringSubmatch(string(res), 1) {
		temp, err := strconv.ParseFloat(v[1], 64)
		if err != nil {
			fmt.Println(err)
			return make(map[string]interface{}), false
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

	defer func() {
		session.Close()
		client.Close()
	}()

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
		fmt.Println(err)
		return make(map[string]interface{}), false
	}
	// 保证关闭输出流
	defer stdout.Close()
	// 运行命令
	if err := cmd.Start(); err != nil {
		return make(map[string]interface{}), false
	}
	// 读取输出结果
	opBytes, err := ioutil.ReadAll(stdout)
	if err != nil {
		fmt.Println(err)
		return make(map[string]interface{}), false
	}
	//log.Println(string(opBytes))

	re := regexp.MustCompile(`Core\s\d:\s+\+(\d+\.?\d*)`)

	data := make(map[string]interface{}, cpuNum)
	coreSum := 0.00
	for k, v := range re.FindAllStringSubmatch(string(opBytes), cpuNum) {
		temp, err := strconv.ParseFloat(v[1], 64)
		if err != nil {
			fmt.Println(err)
			return make(map[string]interface{}), false
		}
		coreSum += temp
		data["CPU"+strconv.Itoa(k)] = temp
	}

	data["CPU"] = coreSum / float64(cpuNum)

	return data, true
}

func dhtSensor(chip string) (map[string]interface{}, bool) {
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

	chip, ok := data["chip"].(string)
	if !ok {
		data["chip"] = "undefined"
	}

	//validation
	if chip == "one" || chip == "two" || chip == "three" || chip == "four" {
		humidity, _ := data["humidity"].(float64)
		temperature, _ := data["temperature"].(float64)
		if humidity == 0.0 && temperature == 0.0 {
			http.Error(w, "温度湿度不能同时等于0", 500)
			return
		}
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
	//w.Write(insertStr)
	io.WriteString(w, "ok")
	fmt.Println(time.Since(start), r.URL)
}
