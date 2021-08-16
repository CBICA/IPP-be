# IPP backend
Runs jobs submitted to the IPP using DRMAA. Configure `API_URL` to the [API server](https://github.com/CBICA/IPP-API) and `DRMAA_DATABASE` to a writable path for the sqlite database.

First the program fetches experiment definitions from queue (`/experiments/queue`). Note that once an experiment is fetched, the API server removes it from the queue. For each experiment, input files are downloaded (`/experiments/{eid}/files`) and placed in `./inputs`. The job is run, the results directory is zipped then uploaded back to the API server (`/experiments/{eid}/results`).

TODO: error handling when job fails
