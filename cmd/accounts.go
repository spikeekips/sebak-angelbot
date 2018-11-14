package cmd

import (
	"container/list"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/stellar/go/keypair"

	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/network"
)

type Account struct {
	KP      *keypair.Full
	Balance common.Amount
}

type AccountManager struct {
	sync.RWMutex

	kp        *keypair.Full
	networkID []byte
	client    *network.HTTP2NetworkClient
	accounts  map[string]*Account
	created   map[string]bool
	unused    *list.List

	checkCreateChan chan ReadyAccount
	createChan      chan []ReadyAccount
	pool            *list.List // []ReadyAccount
}

func NewAccountManager(networkID []byte, kp *keypair.Full, endpoint *common.Endpoint, accounts map[string]*Account) *AccountManager {
	http2Client, _ := common.NewHTTP2Client(
		60*time.Second,
		60*time.Second,
		false,
	)
	client := network.NewHTTP2NetworkClient(endpoint, http2Client)

	return &AccountManager{
		networkID:       networkID,
		kp:              kp,
		client:          client,
		accounts:        accounts,
		created:         map[string]bool{},
		checkCreateChan: make(chan ReadyAccount, 100),
		createChan:      make(chan []ReadyAccount, 100),
		pool:            list.New(),
		unused:          list.New(),
	}
}

func (am *AccountManager) Start() {
	am.startCheckCreatedAccounts()

	for address, _ := range am.created {
		am.unused.PushBack(address)
	}

	log.Debug("unused", "len", am.unused.Len())

	go am.watchCheckCreateAccount()
}

func (am *AccountManager) checkCreatedAccount(id int, account *Account) *Account {
	log.Debug("trying to check account created", "acconnt", account)

	defer func() {
		log.Debug("checked account created", "acconnt", account)
	}()

	ba, err := getAccount(am.client, account.KP.Address())
	if err != nil {
		log.Debug("found error during checking account", "error", err)
		return account
	}

	// fill balance
	account.Balance = ba.Balance

	return nil
}

func (am *AccountManager) checkCreatedAccounts(id int, accountsChan <-chan *Account, errChan chan<- *Account) {
	for account := range accountsChan {
		errChan <- am.checkCreatedAccount(id, account)
	}
}

func (am *AccountManager) startCheckCreatedAccounts() {
	log.Debug("startCheckCreatedAccounts")

	accountsChan := make(chan *Account)
	errChan := make(chan *Account)
	defer close(errChan)

	numWorker := 50

	for i := 0; i < numWorker; i++ {
		go am.checkCreatedAccounts(i, accountsChan, errChan)
	}

	go func() {
		for _, account := range am.accounts {
			accountsChan <- account
		}
		close(accountsChan)
	}()

	var returned int
	var nonAccount []*Account

errorCheck:
	for {
		select {
		case account := <-errChan:
			returned++
			if account != nil {
				nonAccount = append(nonAccount, account)
			}
			if returned == len(am.accounts) {
				break errorCheck
			}
		}
	}

	for _, account := range nonAccount {
		am.created[account.KP.Address()] = false
	}
	for address, _ := range am.accounts {
		if _, ok := am.created[address]; ok {
			continue
		}
		am.created[address] = true
	}

	log.Debug("checking done", "none-exists", len(nonAccount), "accounts", len(am.accounts))

	am.startCreateAccounts(nonAccount)
}

func (am *AccountManager) startCreateAccounts(accounts []*Account) {
	log.Debug("startCreateAccounts")

	limit := 300

	for i := 0; i < int(len(accounts)/limit)+1; i++ {
		s := i * limit
		if s > len(accounts) {
			break
		}
		e := s + limit
		if e > len(accounts) {
			e = len(accounts)
		}
		if s == e {
			break
		}
		fmt.Println(">>", i, s, e, len(accounts), len(accounts[s:e]))

		var ras []ReadyAccount
		for _, account := range accounts[s:e] {
			ras = append(
				ras,
				ReadyAccount{
					Address: account.KP.Address(),
					Balance: common.Amount(1000000000000),
				},
			)
		}

		var sequenceID uint64
		if body, err := am.client.Get("/api/v1/accounts/" + am.kp.Address()); err != nil {
			log.Error("failed to get seed account", "error", err)
		} else {
			var ba block.BlockAccount
			if err = json.Unmarshal(body, &ba); err != nil {
				log.Error("failed to get seed account", "error", err)
				return
			}

			sequenceID = ba.SequenceID
		}
		tx, err := createAccountTransaction(am.networkID, am.kp, sequenceID, time.Second*60, ras...)
		if err != nil {
			log.Error("failed to make transaction", "error", err)
			return
		}

		log.Debug("sent transaction", "transaction", tx.GetHash())
		_, err = am.client.SendTransaction(tx)
		if err != nil {
			log.Error("failed to send transaction", "error", err)
			return
		}

		// check

	endChecking:
		for {
			select {
			case <-time.After(time.Second * 60):
				log.Error("failed to confirmed", "transaction", tx.GetHash())
				break endChecking
			default:
				if _, err := am.client.Get("/api/v1/transactions/" + tx.GetHash()); err != nil {
					time.Sleep(time.Second * 5)
					continue
				}
				break endChecking
			}
		}

		log.Debug("confirmed", "transaction", tx.GetHash())
	}

	log.Debug("created done")
}

type ReadyAccount struct {
	Address string
	Balance common.Amount
}

func (am *AccountManager) CreateAccount(address string, balance common.Amount) {
	am.checkCreateChan <- ReadyAccount{Address: address, Balance: balance}
}

func (am *AccountManager) watchCheckCreateAccount() {
	limit := 300

	go func() {
		ticker := time.NewTicker(time.Second * 3)
		for _ = range ticker.C {
			var pool []ReadyAccount

			var l int
			if am.pool.Len() < 1 {
				continue
			}

			if am.pool.Len() > limit {
				l = limit
			} else {
				l = am.pool.Len()
			}

			var es []*list.Element
			for e := am.pool.Front(); e != nil; e = e.Next() {
				if len(pool) == l {
					break
				}
				pool = append(pool, e.Value.(ReadyAccount))
				es = append(es, e)
			}

			for _, e := range es {
				am.pool.Remove(e)
			}

			if len(pool) < 1 {
				continue
			}
			go func(pool []ReadyAccount) {
				err := am.createAccounts(pool)
				if err == nil {
					return
				}
				for _, ra := range pool {
					am.pool.PushBack(ra)
				}
			}(pool)
		}
	}()

	for {
		select {
		case ra := <-am.checkCreateChan:
			am.pool.PushBack(ra)
		}
	}
}

func (am *AccountManager) checkCreateAccounts(pool []ReadyAccount) {
	if len(pool) < 300 {
		return
	}
}

func (am *AccountManager) createAccounts(pool []ReadyAccount) error {
	source := am.nextSource()
	if source == nil {
		return fmt.Errorf("nextSource() == nil")
	}

	defer func() {
		am.Lock()
		am.unused.PushBack(source.KP.Address())
		am.Unlock()
		log.Debug("unused back", "unused", am.unused.Len(), "accounts", len(am.accounts))
	}()

	log.Debug("nextSource", "source", source.KP.Address(), "pool", len(pool))

	var addresses []string
	for _, a := range pool {
		addresses = append(addresses, a.Address)
	}

	var sequenceID uint64
	if body, err := am.client.Get("/api/v1/accounts/" + source.KP.Address()); err != nil {
		log.Error("failed to get seed account", "error", err)
		return err
	} else {
		var ba block.BlockAccount
		if err = json.Unmarshal(body, &ba); err != nil {
			log.Error("failed to get seed account", "error", err)
			return err
		}

		sequenceID = ba.SequenceID
	}

	tx, err := createAccountTransaction(am.networkID, source.KP, sequenceID, time.Second*60, pool...)
	if err != nil {
		log.Error("failed to make transaction", "error", err)
		return err
	}

	log.Debug("sent transaction", "transaction", tx.GetHash())
	_, err = am.client.SendTransaction(tx)
	if err != nil {
		log.Error("failed to send transaction", "error", err)
		return err
	}

endChecking:
	for {
		select {
		case <-time.After(time.Second * 60):
			err = fmt.Errorf("failed to confirm")
			log.Error("failed to confirmed", "transaction", tx.GetHash())
			break endChecking
		default:
			if _, err := am.client.Get("/api/v1/transactions/" + tx.GetHash()); err != nil {
				time.Sleep(time.Second * 5)
				continue
			}
			break endChecking
		}
	}

	if err != nil {
	}

	return nil
}

func (am *AccountManager) nextSource() *Account {
	am.Lock()
	defer am.Unlock()

	ac := am.unused.Front()
	if ac == nil {
		return nil
	}
	am.unused.Remove(ac)

	address := ac.Value.(string)

	return am.accounts[address]
}
