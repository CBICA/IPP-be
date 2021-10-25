# IPP backend

## Overview
Runs experiments submitted to the IPP using DRMAA (specifically [dgruber/drmaa2os](https://github.com/dgruber/drmaa2os)).

First the program creates a SSH tunnel to the [API server](https://github.com/CBICA/IPP-API) (as the backend routes only accepts connections from localhost) then fetches experiment definitions from queue (`/experiments/queue`). Experiments consist of a host (where to run job), user, app and key-value pairs that contain the app parameters the user submitted in the form. Apps themselves are defined in [experiment definitions](https://github.com/CBICA/IPP-Experiment_Defintions). For each experiment, input files are downloaded (`/experiments/{eid}/files`) and placed in `./inputs`, and the experiment is turned into a job which formats the experiment parameters according to the app definition. The job is then run, the results directory is zipped and uploaded back to the API server (`/experiments/{eid}/results`), then the input files are deleted (`/experiments/{eid}/delete`).

## Installation
In development, just `go run`
```sh
go mod download # install dependancies; only has to be run once
cd src
go run main.go
```
In production, `go build` can be used to create an optimized binary; refer to the Dockerfile
```sh
cd src
go build -o /path/to/optimized/binary
```
Note that `root@localhost` is hardcoded as the SSH login for the API server, and `localhost:5000` is hardcoded as the address of the API web server, running within the SSH host; these can be adjusted at the top of `main.go`.
