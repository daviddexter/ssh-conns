package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/asdine/storm/q"

	"os/exec"
	"os/signal"

	"github.com/asdine/storm"
	sgob "github.com/asdine/storm/codec/gob"
	home "github.com/mitchellh/go-homedir"
	"github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

const (
	cacheDB = "ssh.conns.db"
)

//DB ...
var DB *storm.DB

var restartWatcher chan bool

//Logger ...
type Logger struct {
	ID                   int    `storm:"id,increment"`
	PID                  string `storm:"unique"`
	User                 string
	Name                 string
	Alive                bool      // if connection is still alive. Default true
	TimeRecorded         time.Time // time when connection was recorded
	CheckOutTimeRecorded time.Time // time when connection not alive was recorded
}

func main() {
	restartWatcher = make(chan bool)
	logrus.Infof("--> ssh.conns started at %v", time.Now())
	listener()
}

func listener() {
	logrus.Info("--> ssh.conns:listenning to connections")
	var err error
	hom, _ := home.Dir()
	DB, err = storm.Open(filepath.Join(hom, cacheDB), storm.Codec(sgob.Codec),
		storm.BoltOptions(0600, &bolt.Options{Timeout: 1 * time.Second}))
	if err != nil {
		logrus.Errorf("--> failed to open the cache database %v", err)
		os.Exit(1)
	} else {
		logrus.Info("--> cache found")
	}
	if err := DB.Init(&Logger{}); err != nil {
		logrus.Errorf("--> failed to intialize events bucket,  %v", err)
		os.Exit(1)
	}

	DB.ReIndex(&Logger{})

	go watchTower()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	ticker := time.NewTicker(time.Duration(time.Second * 5))
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			os.Exit(1)
		case r := <-restartWatcher:
			if r {
				time.Sleep(7 * time.Second)
				logrus.Info("--> restarting watchtower")
				go watchTower()
			}
		case <-ticker.C:
			out, err := exec.Command("sudo", "lsof", "-i", ":22").Output()
			if err != nil {
				logrus.Errorf("--> error occured while fetching connections: %v", err.Error())
			}

			objs := []map[string]string{}

			s := bufio.NewScanner(bytes.NewReader(out))
			for s.Scan() {
				line := s.Bytes()
				fields := bytes.Fields(line)
				m := make(map[string]string)
				m["pid"] = fmt.Sprintf("%v", string(fields[1]))
				m["user"] = fmt.Sprintf("%v", string(fields[2]))
				m["name"] = fmt.Sprintf("%v", string(fields[8]))
				objs = append(objs, m)
			}

			//append each object if does not exists into cache
			for _, obj := range objs {
				var e Logger
				DB.Select(q.StrictEq("PID", obj["pid"])).First(&e)
				if len(e.PID) == 0 {
					//write only records whose pid resolve to integer
					if _, err := strconv.Atoi(obj["pid"]); err == nil {
						//create new record
						logrus.Infof("--> creating new obj of PID %v", obj["pid"])
						o := Logger{PID: obj["pid"], User: obj["user"], Name: obj["name"],
							TimeRecorded: time.Now(), Alive: true}
						if err := DB.Save(&o); err != nil {
							logrus.Errorf("--> failed to save object of PID %v", obj["pid"])
						}
					}
				}
			}

		}
	}
}

func watchTower() {
	//recursively checks if cached pid exist. If no, update the cached obj
	c, err := DB.Count(&Logger{})
	if err != nil {
		logrus.Errorf("--> error occured while fetching objects; %v", err)
		time.Sleep(3 * time.Second)
		watchTower()
	}
	if c <= 0 {
		logrus.Warn("--> no objects cached yet")
		time.Sleep(3 * time.Second)
		watchTower()
	} else {
		//get every item and check its life
		var logs []Logger
		DB.Select(q.StrictEq("Alive", true)).Find(&logs)

		for i, log := range logs {
			pid, _ := strconv.Atoi(log.PID)
			p, err := os.FindProcess(pid)
			if err != nil {
				logrus.Errorf("--> error occured while fetching object process for PID %v; Error %v", pid, err)
			} else {
				err := p.Signal(syscall.Signal(0))
				if err != nil {
					updater(log.ID)
				}
			}
			if (i + 1) == len(logs) {
				restartWatcher <- true
			}
		}
	}
}

func updater(id int) {
	//update object
	DB.UpdateField(&Logger{ID: id}, "Alive", false)
	DB.UpdateField(&Logger{ID: id}, "CheckOutTimeRecorded", time.Now())
}
