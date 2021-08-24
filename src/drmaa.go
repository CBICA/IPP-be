package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dgruber/drmaa2interface"
	"github.com/dgruber/drmaa2os"
	_ "github.com/dgruber/drmaa2os/pkg/jobtracker/dockertracker"
	"github.com/elliotchance/sshtunnel"
	"github.com/go-resty/resty/v2"
	"github.com/mholt/archiver/v3"
	"golang.org/x/crypto/ssh"
)

var (
	DRMAA_DATABASE = "testdb.db"
	API_URL        = "http://localhost:5000"
	EXPERIMENT_DIR = "./outputs"
	SSH_ADDR       = "root@localhost"
	REMOTE_ADDR    = "localhost:5000"
	LOCAL_PORT     = "5000"
)

type App struct {
	Executable      string
	Params          map[string]string
	Binopts         map[string]string
	Defaults        map[string]string
	SGEJobResources string
	Container       string
}

type Job struct {
	Command          string
	Args             []string
	Container        string
	WorkingDirectory string
}

type Experiment struct {
	Id     int
	App    string
	Host   string
	User   int
	Params map[string]string
}

func run_job(job Job, sm drmaa2interface.SessionManager, exit_status chan drmaa2interface.JobState) {

	js, err := sm.CreateJobSession("jobsession", "")
	if err != nil {
		fmt.Println("uh oh, delete", DRMAA_DATABASE, "and try again")
		panic(err)
	}

	jt := drmaa2interface.JobTemplate{
		RemoteCommand:    job.Command,
		Args:             job.Args,
		JobCategory:      job.Container,
		WorkingDirectory: job.WorkingDirectory,
	}
	// working dir is experiment dir, 2 levels up includes inputs and outputs
	root := filepath.Dir(filepath.Dir(job.WorkingDirectory))
	fmt.Println("wd", root)
	jt.StageInFiles = map[string]string{
		root: root,
	}
	fmt.Println(jt)
	jr, err := js.RunJob(jt)
	if err != nil {
		panic(err)
	}

	jr.WaitTerminated(drmaa2interface.InfiniteTime)

	exit_status <- jr.GetState()

	js.Close()
	sm.DestroyJobSession("jobsession")
}

func fetch_experiment(experiment Experiment) Job {
	// fetch files for experiment
	eid := strconv.Itoa(experiment.Id)
	resp, err := http.Get(API_URL + "/experiments/" + eid + "/files")
	if err != nil {
		log.Fatalln(err)
	}
	defer resp.Body.Close()
	zipfile := eid + ".zip"
	out, err := os.Create(zipfile)
	if err != nil {
		log.Fatalln(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		log.Fatalln(err)
	}
	// unzip fetched files
	input_dir := filepath.Join("inputs", eid)
	if err := archiver.Unarchive(zipfile, input_dir); err != nil {
		log.Fatalln(err)
	}
	// delete zip
	os.Remove(zipfile)

	data, err := ioutil.ReadFile("../IPP-Experiment_Defintions/" + experiment.App + ".json")
	if err != nil {
		log.Fatalln(err)
	}
	var app App
	json.Unmarshal([]byte(data), &app)

	unused_defaults := app.Defaults
	experdir, _ := filepath.Abs(filepath.Join(EXPERIMENT_DIR, eid))
	// args := []string{app.Executable}
	args := []string{}
	for k, v := range experiment.Params {
		delete(unused_defaults, k) // just try deleting since removing a non-existent entry is a no-op
		if v == "" {
			v = app.Defaults[k]
		}
		if strings.HasPrefix(v, "$experdir") {
			v = filepath.Join(experdir, v[len("$experdir"):])
		} else if strings.HasPrefix(v, "./inputs") {
			v, _ = filepath.Abs(
				filepath.Join("inputs", eid, v[len("./inputs"):]))
		}

		if _, ok := app.Params[k]; ok {
			args = append(args, app.Params[k]+" "+v)
		} else { // it must be in binopts
			args = append(args, app.Binopts[k])
		}
	}
	for k, v := range unused_defaults {
		if strings.HasPrefix(v, "$experdir") {
			v = filepath.Join(experdir, v[len("$experdir"):])
		}
		args = append(args, app.Params[k]+" "+v)
	}
	// fmt.Println(strings.Join(args, " "))
	executable := " "
	if app.Container == "" {
		executable = app.Executable
	}
	ret := Job{
		Command:          executable,
		Args:             args,
		Container:        app.Container,
		WorkingDirectory: experdir,
	}
	return ret

}

func push_results(eid int, uid int) {
	// zip results dir
	experdir, _ := filepath.Abs(filepath.Join(EXPERIMENT_DIR, strconv.Itoa(eid)))
	zipfile := strconv.Itoa(eid) + ".zip"
	// todo could stream files into an archive that is being written to HTTP response w/o writing disk
	// see: https://github.com/mholt/archiver#library-use
	if err := archiver.Archive([]string{experdir}, zipfile); err != nil {
		log.Fatalln(err)
	}
	// upload zip
	client := resty.New()
	resp, err := client.R().
		SetFile("results", zipfile).
		Post(API_URL + "/experiments/" + strconv.Itoa(eid) + "/results")
	fmt.Println(resp)
	if err != nil {
		log.Fatalln(err)
	}

	os.Remove(zipfile)
	os.RemoveAll(experdir)
}

func main() {
	// Setup the tunnel, but do not yet start it yet.
	tunnel := sshtunnel.NewSSHTunnel(
		// User and host of tunnel server, it will default to port 22
		// if not specified.
		SSH_ADDR,

		// Pick ONE of the following authentication methods:
		// sshtunnel.PrivateKeyFile("path/to/private/key.pem"), // 1. private key
		ssh.Password("root"), // 2. password
		// sshtunnel.SSHAgent(),                                // 3. ssh-agent

		// The destination host and port of the actual server.
		REMOTE_ADDR,

		// The local port you want to bind the remote port to.
		// Specifying "0" will lead to a random port.
		LOCAL_PORT,
	)

	// You can provide a logger for debugging, or remove this line to
	// make it silent.
	tunnel.Log = log.New(os.Stdout, "", log.Ldate|log.Lmicroseconds)

	// Start the server in the background. You will need to wait a
	// small amount of time for it to bind to the localhost port
	// before you can start sending connections.
	go tunnel.Start()
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(API_URL + "/experiments/queue")
	if err != nil {
		log.Fatalln(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
	}
	var queue []Experiment
	json.Unmarshal([]byte(body), &queue)

	for _, experiment := range queue {

		job := fetch_experiment(experiment)
		exit_status := make(chan drmaa2interface.JobState)
		switch experiment.Host {
		case "localhost":
			fmt.Println(job)
			sm, err := drmaa2os.NewDockerSessionManager(DRMAA_DATABASE)
			if err != nil {
				panic(err)
			}
			go run_job(job, sm, exit_status)
		case "cubic":
			fmt.Println(job)
			sm, err := drmaa2os.NewLibDRMAASessionManager(DRMAA_DATABASE)
			if err != nil {
				panic(err)
			}
			go run_job(job, sm, exit_status)
		default:
			log.Fatalln("unknown host:", experiment.Host)
		}

		result := <-exit_status
		switch result {
		case drmaa2interface.Failed:
			fmt.Println("Failed to execute job successfully")
		case drmaa2interface.Done:
			fmt.Println("Completed successfully")
			push_results(experiment.Id, experiment.User)
		}

	}
}
