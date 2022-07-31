## How to use the load-pmm-dump.sh script

There are three ways to run the script (all of them require at least one parameter -- the ticket number or container name string):

`./load-pmm-dump.sh CS0012345`

This will assume latest PMM v2 server version, and will deploy two containers: `pmm-data-CS0012345` and `pmm-server-CS0012345`

`./load-pmm-dump.sh CS0012345 2.26.0`

This will use the second argument as PMM server Docker version tag

`./load-pmm-dump.sh CS0012345 /path/to/pmm-dump-1653879141.tar.gz`

This will get the PMM server Docker version tag from the .tar.gz file metadata, and it will use it for the containers.

After it's done, the tool will output some helpful information and copy/paste-ready commands. For example:

```
## USEFUL INFORMATION AND COMMANDS.

## Port 443 is exported to:  49196

## Use the following for port redirection from your local machine:
ssh -L 8443:127.0.0.1:49196  highram

## To import a PMM dump:
pmm-dump import --allow-insecure-certs --pmm-url=https://admin:admin@127.0.0.1:49195 --dump-path=/bigdisk/agustin/load-pmm-dump-files/pmm-dump-1653879141.tar.gz

## Use the following to get human readable dates from a Unix timestamp:
date -d @1657948064

## Increase 'Data Retention' in the advanced settings if the samples are older than the default 30 days.

## To destroy docker containers:
docker rm -vf pmm-data-CS0027251 pmm-server-CS0027251
```

