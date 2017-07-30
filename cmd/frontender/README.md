## frontender

Commandline interface app for the frontender service

## Installation
```shell
$ go get github.com/orijtech/frontender/cmd/frontender
```

## Usage
- [X] Get the URLs for your backend servers
- [X] Figure out what domains your frontend's identity will take on e.g
rsp.orijtech.com, code.orijtech.com -- this is optional if you are running
frontender as a local non-HTTPS server

### Sample usage serving in production
```shell
$ frontender -csv-backends http://localhost:8889,http://localhost:8998,http://localhost:8994 -backend-ping-period 8m -domains orijtech.com,code.orijtech.com,home.orijtech.com
```

### Sample usage serving locally as non-HTTPS
```
$ frontender -csv-backends http://localhost:8889,http://localhost:8998,http://localhost:8994 -backend-ping-period 2m -http1
```

