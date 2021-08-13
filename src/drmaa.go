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

	"github.com/dgruber/drmaa2interface"
	"github.com/dgruber/drmaa2os"
	_ "github.com/dgruber/drmaa2os/pkg/jobtracker/dockertracker"
	"github.com/mholt/archiver/v3"
)

var DRMAA_DATABASE = "testdb.db"
var API_URL = "http://localhost:3330"
var EXPERIMENT_DIR = "./outputs"

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
	fmt.Println("Uploading", eid)
	// zip results dir
	experdir, _ := filepath.Abs(filepath.Join(EXPERIMENT_DIR, strconv.Itoa(eid)))
	zipfile := strconv.Itoa(eid) + ".zip"
	// todo could stream files into an archive that is being written to HTTP response w/o writing disk
	// see: https://github.com/mholt/archiver#library-use
	if err := archiver.Archive([]string{experdir}, zipfile); err != nil {
		log.Fatalln(err)
	}
	// upload zip
	// resp, err := http.Post(API_URL+"/experiments/"+strconv.Itoa(eid)+"/results", "application/zip",
	// 	bytes.NewReader([]byte(zipfile)))
	// if err != nil {
	// 	log.Fatalln(err)
	// }
	// defer resp.Body.Close()
	// delete zip
	// os.Remove(zipfile)
	// delete results dir
	// os.RemoveAll(experdir)
}

func main() {
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
