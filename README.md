# IPP backend
Runs jobs submitted to the IPP using DRMAA (specifically [dgruber/drmaa2os](https://github.com/dgruber/drmaa2os)). Configure `API_URL` to the [API server](https://github.com/CBICA/IPP-API) and `DRMAA_DATABASE` to a writable path for a sqlite database the drmaa2os library maintains session info in.

Unless the backend server and API server are running on the same host, a SSH tunnel is necessary to access API endpoints as they only respond to localhost.

First the program fetches experiment definitions from queue (`/experiments/queue`). For each experiment, input files are downloaded (`/experiments/{eid}/files`) and placed in `./inputs`. The job is run, the results directory is zipped then uploaded back to the API server (`/experiments/{eid}/results`).

TODO: error handling when job fails
