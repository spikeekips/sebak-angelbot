# SEBAK AngleBot

This is the helper to use SEBAK *Testnet*

## Features

* Creating new account

## Installation

```
$ git clone https://github.com/spikeekips/sebak-angelbot
$ cd sebak-angelbot
$ go get -u boscoin.io/sebak
$ go build
```

## Deploy

```sh
$ ./sebak-angelbot run -h
run sebak-angelbot

Usage:
  ./sebak-angelbot run [flags]

Flags:
      --bind string             bind address (default "http://localhost:23456")
  -h, --help                    help for run
      --log-level string        log level, {crit, error, warn, info, debug} (default "info")
      --log-output string       set log output file
      --network-id string       network id
      --rate-limit list         rate limit: [<ip>=]<limit>-<period>, ex) '10-S' '3.3.3.3=1000-M'
      --sebak-endpoint string   sebak endpoint uri (default "https://localhost:12345")
      --secret-seed string      secret seed of master account
      --sources string          source account list file
      --tls-cert string         tls certificate file (default "sebak.crt")
      --tls-key string          tls key file (default "sebak.key")
      --verbose                 verbose
```

If you environment is,

* sebak node is running on: https://localhost:12345
* sebak node's secret seed: SBXBRFM4UDBHREM2XRM6IIOXNR52N6NAKWIMR7MR4XMNJ5VA4WC27QDY
* sebak network network-id: "test-sebak-network"
* sebak-angelbot will be running on: https://localhost:23456

```
$ sebak-angelbot run \
	--bind http://0.0.0.0:23456 \
	--network-id 'test-sebak-network' \
	--secret-seed SBXBRFM4UDBHREM2XRM6IIOXNR52N6NAKWIMR7MR4XMNJ5VA4WC27QDY \
	--log-level debug \
	--sebak-endpoint https://localhost:12345 \
    --sources /tmp/sources.txt \
    $*
```

* `--sources` should be set, this file contains the list of secret seed to distribute balances for new account.

```
SBO3ATFYI2VALX3CEMF5YYR6EYHLRQV5NBFM7THO2ZFFEFXSYQUQGUVJ
SDEYGI6HT6IAZ7FR5ZAPOBM2GPYXMVPHK67OAAWQVDINUYM2QTWYBYFT
```

You can set secret seeds as many as you want.

## Usage

Just request to angelbot. If you want to create new account that has,

* Address: `GA5DR66ZVT7SFAQWRQYPI5V6XNCCWN57Y4HP4CNBBGH4LFHQMT7TTE6M`
* Initial balance: `100,000 BOS`(`1,000,000,000,000 GON`)

> You can make new keypair with [sebak](https://github.com/bosnet/sebak) or  [stellar SDK](https://www.stellar.org/developers/reference/). SEBAK shared with the same keypair with [stellar](https://www.stellar.org/developers/).

```
$ time curl \
    --insecure \
    -s \
    "https://localhost:8090/account/GA5DR66ZVT7SFAQWRQYPI5V6XNCCWN57Y4HP4CNBBGH4LFHQMT7TTE6M"
```

You can set the initial `balance` by querystring, `balance=9990000000`, `999 BOS`. The unit of balance is `GON`, not `BOS`.

```
$ time curl \
    --insecure \
    -s \
    "https://localhost:8090/account/GA5DR66ZVT7SFAQWRQYPI5V6XNCCWN57Y4HP4CNBBGH4LFHQMT7TTE6M?balance=9990000000"
```

You can set the `timeout` by querystring, it will wait until when account is created.

```
$ time curl \
    --insecure \
    -s \
    "https://localhost:8090/account/GA5DR66ZVT7SFAQWRQYPI5V6XNCCWN57Y4HP4CNBBGH4LFHQMT7TTE6M?balance=9990000000&timeout=1s"
```
> The timeout format can be found at https://golang.org/pkg/time/#ParseDuration .

