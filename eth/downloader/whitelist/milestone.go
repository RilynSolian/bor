package whitelist

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/flags"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type milestone struct {
	finality[*rawdb.Milestone]

	//todo: need persistence
	LockedSprintNumber uint64              // Locked sprint number
	LockedSprintHash   common.Hash         //Hash for the locked endBlock
	Locked             bool                //
	LockedMilestoneIDs map[string]struct{} //list of milestone ids

}

type milestoneService interface {
	finalityService
	GetMilestoneIDsList() []string
	RemoveMilestoneID(milestoneId string)
	LockMutex(endBlockNum uint64) bool
	UnlockMutex(doLock bool, milestoneId string, endBlockHash common.Hash)
	UnlockSprint(endBlockNum uint64)
}

// IsValidChain checks the validity of chain by comparing it
// against the local milestone entries
func (m *milestone) IsValidChain(currentHeader *types.Header, chain []*types.Header) bool {
	m.finality.RLock()
	defer m.finality.RUnlock()

	//Checking for the milestone flag
	if !flags.Milestone {
		return true
	}

	if !m.finality.IsValidChain(currentHeader, chain) {
		return false
	}

	if m.Locked && !m.IsReorgAllowed(chain, m.LockedSprintNumber, m.LockedSprintHash) {
		return false
	}

	return true
}

// IsValidPeer checks if the chain we're about to receive from a peer is valid or not
// in terms of reorgs. We won't reorg beyond the last bor finality submitted to mainchain.
func (m *milestone) IsValidPeer(remoteHeader *types.Header, fetchHeadersByNumber func(number uint64, amount int, skip int, reverse bool) ([]*types.Header, []common.Hash, error)) (bool, error) {

	if !flags.Milestone {
		return true, nil
	}

	return m.finality.IsValidPeer(remoteHeader, fetchHeadersByNumber)
}

func (m *milestone) Process(block uint64, hash common.Hash) {
	m.finality.Lock()
	defer m.finality.Unlock()

	m.finality.Process(block, hash)

	m.UnlockSprint(block)
}

// This function will Lock the mutex at the time of voting
// fixme: get rid of it
func (m *milestone) LockMutex(endBlockNum uint64) bool {
	m.finality.Lock()

	if m.doExist && endBlockNum <= m.Number { //if endNum is less than whitelisted milestone, then we won't lock the sprint
		// todo: add endBlockNum and m.Number as values - the same below
		log.Warn("endBlockNum <= m.Number")

		return false
	}

	if m.Locked && endBlockNum != m.LockedSprintNumber {
		if endBlockNum < m.LockedSprintNumber {
			log.Warn("endBlockNum < m.LockedSprintNumber")
			return false
		}

		log.Warn("endBlockNum > m.LockedSprintNumber")
		m.UnlockSprint(m.LockedSprintNumber)
		m.Locked = false
	}

	m.LockedSprintNumber = endBlockNum

	return true
}

// This function will unlock the mutex locked in LockMutex
// fixme: get rid of it
func (m *milestone) UnlockMutex(doLock bool, milestoneId string, endBlockHash common.Hash) {
	m.Locked = m.Locked || doLock

	if doLock {
		m.LockedSprintHash = endBlockHash
		m.LockedMilestoneIDs[milestoneId] = struct{}{}
	}

	rawdb.WriteLockField(m.db, m.Locked, m.LockedSprintNumber, m.LockedSprintHash, m.LockedMilestoneIDs)

	m.finality.Unlock()
}

// This function will unlock the locked sprint
func (m *milestone) UnlockSprint(endBlockNum uint64) {
	if endBlockNum < m.LockedSprintNumber {
		return
	}

	m.Locked = false
	m.purgeMilestoneIDsList()
	rawdb.WriteLockField(m.db, m.Locked, m.LockedSprintNumber, m.LockedSprintHash, m.LockedMilestoneIDs)
}

// This function will remove the stored milestoneID
func (m *milestone) RemoveMilestoneID(milestoneId string) {
	m.finality.Lock()

	delete(m.LockedMilestoneIDs, milestoneId)

	if len(m.LockedMilestoneIDs) == 0 {
		m.Locked = false
	}

	rawdb.WriteLockField(m.db, m.Locked, m.LockedSprintNumber, m.LockedSprintHash, m.LockedMilestoneIDs)

	m.finality.Unlock()
}

// This will check whether the incoming chain matches the locked sprint hash
func (m *milestone) IsReorgAllowed(chain []*types.Header, lockedSprintNumber uint64, lockedSprintHash common.Hash) bool {

	if chain[len(chain)-1].Number.Uint64() <= lockedSprintNumber { //Can't reorg if the end block of incoming
		return false //chain is less than locked sprint number
	}

	for i := 0; i < len(chain); i++ {
		if chain[i].Number.Uint64() == lockedSprintNumber {
			return chain[i].Hash() == lockedSprintHash

		}
	}

	return true
}

// This will return the list of milestoneIDs stored.
func (m *milestone) GetMilestoneIDsList() []string {
	m.finality.RLock()
	defer m.finality.RUnlock()

	// fixme: use generics :)
	keys := make([]string, 0, len(m.LockedMilestoneIDs))
	for key := range m.LockedMilestoneIDs {
		keys = append(keys, key)
	}

	return keys
}

// This is remove the milestoneIDs stored in the list.
func (m *milestone) purgeMilestoneIDsList() {
	m.LockedMilestoneIDs = make(map[string]struct{})
}

func (m *milestone) block() (uint64, common.Hash) {
	return m.Number, m.Hash
}
