package godaemon

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

var (
	appName string
	pidFile string
	pidVal  int
)

func init() {
	file, _ := filepath.Abs(os.Args[0])
	appPath := filepath.Dir(file)
	appName = filepath.Base(file)

	pidFile = appPath + "/" + appName + ".pid"
	if os.Getenv("__Daemon") != "true" { //master
		log.Println("+++---------------+++")

		cmd := "start" //缺省为start
		if l := len(os.Args); l > 1 {
			cmd = os.Args[l-1]
		}
		switch cmd {
		case "start":
			if isRunning() {
				log.Printf("[%d] %s is running\n", pidVal, appName)
			} else { //fork daemon进程
				if err := forkDaemon(); err != nil {
					log.Fatal(err)
				}
			}
		case "restart": //重启:
			if !isRunning() {
				log.Printf("%s not running\n", appName)
			} else {
				log.Printf("[%d] %s restart now\n", pidVal, appName)
				restart(pidVal)
			}
		case "stop": //停止
			if !isRunning() {
				log.Printf("%s not running\n", appName)
			} else {

				syscall.Kill(pidVal, syscall.SIGTERM) //kill
			}
		case "-h":
			fmt.Printf("Usage:\n\r %s start|restart|stop|-h\n", appName)
		default: //其它不识别的参数
			return //返回至调用方
		}
		//主进程退出
		os.Exit(0)
	}
	go handleSignals()
}

//检查pidFile是否存在以及文件里的pid是否存活
func isRunning() bool {
	if mf, err := os.Open(pidFile); err == nil {
		pid, _ := ioutil.ReadAll(mf)
		pidVal, _ = strconv.Atoi(string(pid))
	}
	running := false
	if pidVal > 0 {
		if err := syscall.Kill(pidVal, 0); err == nil { //发一个信号为0到指定进程ID，如果没有错误发生，表示进程存活
			running = true
		}
	}
	return running
}

//保存pid
func savePid(pid int) error {
	file, err := os.OpenFile(pidFile, os.O_CREATE|os.O_WRONLY, os.ModePerm)
	if err != nil {
		return err
	}
	defer file.Close()
	file.WriteString(strconv.Itoa(pid))
	return nil
}

//捕获系统信号
func handleSignals() {
	signals := make(chan os.Signal)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	var err error
	for {
		sig := <-signals
		switch sig {
		case syscall.SIGHUP: //重启
			err = fork()
			if err != nil {
				log.Fatalln(err)
			}
		case syscall.SIGINT:
			fallthrough
		case syscall.SIGTERM:
			log.Printf("[%d] %s stop graceful", os.Getpid(), appName)
			shutdown()
			os.Exit(1)
		}
	}
}

//forkDaemon,当checkPid为true时，检查是否有存活的，有则不执行
func forkDaemon() error {
	args := os.Args
	os.Setenv("__Daemon", "true")
	procAttr := &syscall.ProcAttr{
		Env:   os.Environ(),
		Files: []uintptr{os.Stdin.Fd(), os.Stdout.Fd(), os.Stderr.Fd()},
	}
	pid, err := syscall.ForkExec(os.Args[0], args, procAttr)
	if err != nil {
		return err
	}
	log.Printf("[%d] %s start daemon\n", pid, appName)
	savePid(pid)
	return nil
}

//重启(先发送kill -HUP到运行进程，手工重启daemon ...当有运行的进程时，daemon不启动)
func restart(pid int) {
	syscall.Kill(pid, syscall.SIGHUP) //kill -HUP, daemon only时，会直接退出
	fork := make(chan bool, 1)
	go func() { //循环，查看pidFile是否存在，不存在或值已改变，发送消息
		for {
			f, err := os.Open(pidFile)
			if err != nil || os.IsNotExist(err) { //文件已不存在
				fork <- true
				break
			} else {
				pidVal, _ := ioutil.ReadAll(f)
				if strconv.Itoa(pid) != string(pidVal) {
					fork <- false
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
	//处理结果
	select {
	case r := <-fork:
		if r {
			forkDaemon()
		}
	case <-time.After(time.Second * 5):
		log.Fatalln("restart timeout")
	}

}

//fork一个新的进程
func fork() error {
	os.Setenv("_GRACEFUL_RESTART", "true")
	execSpec := &syscall.ProcAttr{
		Env:   os.Environ(),
		Files: []uintptr{os.Stdin.Fd(), os.Stdout.Fd(), os.Stderr.Fd()},
	}
	pid, err := syscall.ForkExec(os.Args[0], os.Args, execSpec)
	if err != nil {
		return err
	}
	savePid(pid)
	log.Printf("[%d] %s fork ok\n", pid, appName)
	return nil
}

//关闭服务
func shutdown() {
	log.Printf("[%d] %s stopped.", os.Getpid(), appName)
}
