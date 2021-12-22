package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func main() {
	projectPath := getProjectPath()
	config := getConfig(projectPath)

	os.Chdir(projectPath)
	os.Chdir("..")

	execCmd("git pull")

	needPush := false
	for _, data := range config.DataArray {
		cmd := exec.Command("sh", "-c", data.GetIpCmd)
		stdout, err := cmd.CombinedOutput()
		if err != nil {
			log.Println("获取ip地址失败", err)
			continue
		}

		ipArray := strings.Split(string(stdout), "\n")
		if len(ipArray) == 0 {
			continue
		}

		var ipMap = make(map[string]struct{})
		for _, ip := range ipArray {
			ipMap[ip] = struct{}{}
		}

		isChanged := writeFile(ipMap, ipArray[0], data.FileName, data.Host)

		if isChanged {
			log.Println(data.FileName + " 修改成功")
			execCmd("git add " + data.FileName)
			needPush = true
		} else {
			log.Println(data.FileName + " 无需修改")
		}
	}

	if needPush {
		execCmd("git commit -m 'auto update'")
		execCmd("git push")
	}
}

func getConfig(projectPath string) Config {
	configBytes, err := ioutil.ReadFile(projectPath + "/config.json")
	if err != nil {
		log.Fatal("读取配置文件失败：", err)
	}
	config := Config{}
	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		log.Fatal("配置转换json失败：", err)
	}
	return config
}

type Config struct {
	DataArray []struct{ Env, GetIpCmd, FileName, Host string } `json:"data"`
}

func getProjectPath() string {
	curDir, _ := os.Getwd()
	if strings.Contains(curDir, "autoUpdateIP") {
		return curDir
	}

	file, _ := exec.LookPath(os.Args[0])
	path, _ := filepath.Abs(file)
	index := strings.LastIndex(path, string(os.PathSeparator))
	return path[:index]
}

func execCmd(cmdString string) {
	cmd := exec.Command("sh", "-c", cmdString)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatal(string(stdout), err)
	}
	log.Print(string(stdout))
}

// 读取对应的.etchost文件，用正则检取后替换
func writeFile(ipMap map[string]struct{}, ip string, fileName string, host string) bool {
	rwFile, err := os.OpenFile(fileName, os.O_RDWR, 0766)
	if err != nil {
		log.Fatal("打开文件发生错误:", err)
	}
	defer rwFile.Close()

	reader := bufio.NewReader(rwFile)

	/*
	 * nextLine处理是防止EOF被写入的内容覆盖掉，导致无限循环的bug
	 */
	nextLine, readSliceErr := reader.ReadSlice('\n')
	if readSliceErr != nil {
		if readSliceErr == io.EOF {
			log.Println(fileName + "为空白文件")
			return false
		} else {
			log.Fatal("读取"+fileName+"时发生错误：", readSliceErr)
		}
	}

	hasChanged := false
	var offset int64 = 0
	var lineLength int
	for {
		curLine := nextLine
		lineLength = len(curLine)
		needWriteOldLine := true

		nextLine, readSliceErr = reader.ReadSlice('\n')

		regexPtr, _ := regexp.Compile("consul|apollo|kibana|rabbitmq|jenkins|log")
		if regexPtr.Match(curLine) {
			regexPtr, _ = regexp.Compile("(\\d+\\.){3}\\d+")
			curIp := string(regexPtr.Find(curLine))

			_, ok := ipMap[curIp]
			ok = false
			if !ok && isCurIpInvalid(curIp, host) {
				newLine := regexPtr.ReplaceAll(curLine, []byte(ip))
				_, err = rwFile.WriteAt(newLine, offset)
				if err != nil {
					log.Fatal("写入文件时发生错误：", err)
				}
				lineLength = len(newLine)
				needWriteOldLine = false
				hasChanged = true
			}
		}
		if needWriteOldLine && hasChanged {
			_, err = rwFile.WriteAt(curLine, offset)
			if err != nil {
				log.Fatal("写入文件时发生错误：", err)
			}
		}
		offset += int64(lineLength)

		if readSliceErr != nil {
			if readSliceErr == io.EOF {
				break
			} else {
				log.Fatal("读取"+fileName+"时发生错误：", readSliceErr)
			}
		}
	}

	err = rwFile.Truncate(offset)
	if err != nil {
		log.Fatal("truncate file at "+strconv.Itoa(int(offset))+" fail:", err)
	}

	return hasChanged
}

func isCurIpInvalid(ip string, host string) (result bool) {
	fmt.Print("开始验证" + ip + "是否失效...")
	defer func() { fmt.Println("结果为: " + strconv.FormatBool(result)) }()
	request, _ := http.NewRequest("GET", "http://"+ip, nil)
	request.Header.Set("host", host)
	request.Host = host

	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	response, err := client.Do(request)
	if err != nil {
		fmt.Println()
		log.Println("http请求失败", err)
		return true
	}
	return response.StatusCode != 200
}
