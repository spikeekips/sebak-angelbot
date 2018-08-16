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

* sebak node is running on: https://192.168.99.110:12000
* sebak node's secret seed: SBXBRFM4UDBHREM2XRM6IIOXNR52N6NAKWIMR7MR4XMNJ5VA4WC27QDY
* sebak network network-id: test sebak-network
* sebak-angelbot will be running on: https://localhost:8090

```
$ sebak-anglebot run \
	--bind localhost:8090 \
	--network-id 'test sebak-network' \
	--secret-seed SBXBRFM4UDBHREM2XRM6IIOXNR52N6NAKWIMR7MR4XMNJ5VA4WC27QDY \
	--log-level debug \
	--tls-cert ./sebak.crt \
	--tls-key ./sebak.key  \
	--sebak-endpoint https://192.168.99.110:12000 \
    $*
```

## Usage

Just send a transaction to angelbot. If you want to create new account that has,

* address: GA5DR66ZVT7SFAQWRQYPI5V6XNCCWN57Y4HP4CNBBGH4LFHQMT7TTE6M
* initial balance: 100,000 BOS

```
$ time curl \
    --insecure \
    -s \
    -XPOST \
    -d '' \
    "https://localhost:8090/account/GA5DR66ZVT7SFAQWRQYPI5V6XNCCWN57Y4HP4CNBBGH4LFHQMT7TTE6M"
```

> You can set the initial balance by set the querystring, `?balance=999`.
