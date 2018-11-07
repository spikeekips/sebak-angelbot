# SEBAK AngleBot

This is the helper to use SEBAK *Testnet*

## Features

* Creating new account

## Installation

```
$ go get github.com/spikeekips/sebak-angelbot
```

## Deploy

If you environment is,

* sebak node is running on: https://localhost:12345
* sebak node's secret seed: SBXBRFM4UDBHREM2XRM6IIOXNR52N6NAKWIMR7MR4XMNJ5VA4WC27QDY
* sebak network network-id: "test-sebak-network"
* sebak-angelbot will be running on: https://localhost:23456

```
$ sebak-anglebot run \
	--bind localhost:23456 \
	--network-id 'test-sebak-network' \
	--secret-seed SBXBRFM4UDBHREM2XRM6IIOXNR52N6NAKWIMR7MR4XMNJ5VA4WC27QDY \
	--log-level debug \
	--tls-cert ./sebak.crt \
	--tls-key ./sebak.key  \
	--sebak-endpoint https://localhost:12345 \
    $*
```

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
