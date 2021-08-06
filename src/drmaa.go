package main

import (
	"archive/zip"
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
)

var DRMAA_DATABASE = "testdb.db"
var API_URL = "http://localhost:3330"

type App struct {
	Executable      string
	Params          map[string]string
	Binopts         map[string]string
	Defaults        map[string]string
	SGEJobResources string
	Container       string
}

type Job struct {
	Command   string
	Args      []string
	Container string
}

type Experiment struct {
	Id     int
	App    string
	Host   string
	User   int
	Params map[string]string
}

func Unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	os.MkdirAll(dest, 0755)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
		} else {
			os.MkdirAll(filepath.Dir(path), 0755)
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}

func run_job(job Job) {
	sm, err := drmaa2os.NewDockerSessionManager(DRMAA_DATABASE)
	if err != nil {
		panic(err)
	}

	js, err := sm.CreateJobSession("jobsession", "")
	if err != nil {
		panic(err)
	}

	jt := drmaa2interface.JobTemplate{
		RemoteCommand: job.Command,
		Args:          job.Args,
		JobCategory:   job.Container,
	}
	jr, err := js.RunJob(jt)
	if err != nil {
		panic(err)
	}

	jr.WaitTerminated(drmaa2interface.InfiniteTime)

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
	out, err := os.Create(eid + ".zip")
	if err != nil {
		log.Fatalln(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		log.Fatalln(err)
	}
	// unzip fetched files
	if err := Unzip(eid+".zip", eid); err != nil {
		log.Fatalln(err)
	}

	data, err := ioutil.ReadFile("../config/" + experiment.App + ".json")
	if err != nil {
		log.Fatalln(err)
	}
	var app App
	json.Unmarshal([]byte(data), &app)

	// args := []string{app.Executable}
	args := []string{}
	for k, v := range experiment.Params {
		if v == "" {
			v = app.Defaults[k]
		}
		// if v starts with /var/uploads/
		if strings.HasPrefix(v, "/var/uploads/") {
			v = filepath.Join(eid, v)
		}
		if _, ok := app.Params[k]; ok {
			args = append(args, app.Params[k]+" "+v)
		} else { // it must be in binopts
			args = append(args, app.Binopts[k])
		}
	}
	// fmt.Println(strings.Join(args, " "))
	ret := Job{
		Command:   app.Executable,
		Args:      args,
		Container: app.Container,
	}
	return ret

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

		// fetch_experiment(experiment)
		// fmt.Println(fetch_experiment(experiment))
		run_job(fetch_experiment(experiment))

	}
}
