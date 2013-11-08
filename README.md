# hostpool

A "thread pool" manager of DigitalOcean hosts. It can be used for on-demand CI workers, and runs on Heroku.

There is an HTTP endpoint to GET a host, in which depending on pool size (concurrency) you may wait in line, but eventually wait while a host is provisioned. While you wait, the connection remains open. Once provisioned, the IP of the host is returned and the connection is closed. The provisioned host will automatically be destroyed after a timeout. Hostpool ensures only a certain number of hosts are active at any time.

Configuration is done via environment variables:

* `CLIENT_ID`: DigitalOcean client ID
* `API_KEY`: DigitalOcean API key
* `PORT`: Port to bind on
* `NAME`: Name used to prefix Droplets owned by hostpool
* `CONCURRENCY`: Number of concurrent active hosts allowed (pool size)
* `TIMEOUT`: Minutes before a host is automatically destroyed
* `IMAGE`: ID of your DigitalOcean image to use for the hosts
* `KEY`: ID of a DigitalOcean SSH key to use with the hosts

All configuration is required except for `KEY`.

## License

MIT