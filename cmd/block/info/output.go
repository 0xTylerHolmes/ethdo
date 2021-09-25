// Copyright © 2019, 2020, 2021 Weald Technology Trading
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package blockinfo

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/wealdtech/go-string2eth"
)

type dataOut struct {
	debug         bool
	verbose       bool
	eth2Client    eth2client.Service
	genesisTime   time.Time
	slotDuration  time.Duration
	slotsPerEpoch uint64
}

func output(ctx context.Context, data *dataOut) (string, error) {
	if data == nil {
		return "", errors.New("no data")
	}

	return "", nil
}

func outputBlockGeneral(ctx context.Context,
	verbose bool,
	slot phase0.Slot,
	blockRoot phase0.Root,
	bodyRoot phase0.Root,
	parentRoot phase0.Root,
	stateRoot phase0.Root,
	graffiti []byte,
	genesisTime time.Time,
	slotDuration time.Duration,
	slotsPerEpoch uint64,
) (
	string,
	error,
) {
	res := strings.Builder{}

	res.WriteString(fmt.Sprintf("Slot: %d\n", slot))
	res.WriteString(fmt.Sprintf("Epoch: %d\n", phase0.Epoch(uint64(slot)/slotsPerEpoch)))
	res.WriteString(fmt.Sprintf("Timestamp: %v\n", time.Unix(genesisTime.Unix()+int64(slot)*int64(slotDuration.Seconds()), 0)))
	res.WriteString(fmt.Sprintf("Block root: %#x\n", blockRoot))
	if verbose {
		res.WriteString(fmt.Sprintf("Body root: %#x\n", bodyRoot))
		res.WriteString(fmt.Sprintf("Parent root: %#x\n", parentRoot))
		res.WriteString(fmt.Sprintf("State root: %#x\n", stateRoot))
	}
	if len(graffiti) > 0 && hex.EncodeToString(graffiti) != "0000000000000000000000000000000000000000000000000000000000000000" {
		if utf8.Valid(graffiti) {
			res.WriteString(fmt.Sprintf("Graffiti: %s\n", string(graffiti)))
		} else {
			res.WriteString(fmt.Sprintf("Graffiti: %#x\n", graffiti))
		}
	}

	return res.String(), nil
}

func outputBlockETH1Data(ctx context.Context, eth1Data *phase0.ETH1Data) (string, error) {
	res := strings.Builder{}

	res.WriteString(fmt.Sprintf("Ethereum 1 deposit count: %d\n", eth1Data.DepositCount))
	res.WriteString(fmt.Sprintf("Ethereum 1 deposit root: %#x\n", eth1Data.DepositRoot))
	res.WriteString(fmt.Sprintf("Ethereum 1 block hash: %#x\n", eth1Data.BlockHash))

	return res.String(), nil
}

func outputBlockAttestations(ctx context.Context, eth2Client eth2client.Service, verbose bool, attestations []*phase0.Attestation) (string, error) {
	res := strings.Builder{}

	validatorCommittees := make(map[phase0.Slot]map[phase0.CommitteeIndex][]phase0.ValidatorIndex)
	res.WriteString(fmt.Sprintf("Attestations: %d\n", len(attestations)))
	if verbose {
		beaconCommitteesProvider, isProvider := eth2Client.(eth2client.BeaconCommitteesProvider)
		if isProvider {
			for i, att := range attestations {
				res.WriteString(fmt.Sprintf("  %d:\n", i))

				// Fetch committees for this epoch if not already obtained.
				committees, exists := validatorCommittees[att.Data.Slot]
				if !exists {
					beaconCommittees, err := beaconCommitteesProvider.BeaconCommittees(ctx, fmt.Sprintf("%d", att.Data.Slot))
					if err != nil {
						return "", errors.Wrap(err, "failed to obtain beacon committees")
					}
					for _, beaconCommittee := range beaconCommittees {
						if _, exists := validatorCommittees[beaconCommittee.Slot]; !exists {
							validatorCommittees[beaconCommittee.Slot] = make(map[phase0.CommitteeIndex][]phase0.ValidatorIndex)
						}
						validatorCommittees[beaconCommittee.Slot][beaconCommittee.Index] = beaconCommittee.Validators
					}
					committees = validatorCommittees[att.Data.Slot]
				}

				res.WriteString(fmt.Sprintf("    Committee index: %d\n", att.Data.Index))
				res.WriteString(fmt.Sprintf("    Attesters: %d/%d\n", att.AggregationBits.Count(), att.AggregationBits.Len()))
				res.WriteString(fmt.Sprintf("    Aggregation bits: %s\n", bitlistToString(att.AggregationBits)))
				res.WriteString(fmt.Sprintf("    Attesting indices: %s\n", attestingIndices(att.AggregationBits, committees[att.Data.Index])))
				res.WriteString(fmt.Sprintf("    Slot: %d\n", att.Data.Slot))
				res.WriteString(fmt.Sprintf("    Beacon block root: %#x\n", att.Data.BeaconBlockRoot))
				res.WriteString(fmt.Sprintf("    Source epoch: %d\n", att.Data.Source.Epoch))
				res.WriteString(fmt.Sprintf("    Source root: %#x\n", att.Data.Source.Root))
				res.WriteString(fmt.Sprintf("    Target epoch: %d\n", att.Data.Target.Epoch))
				res.WriteString(fmt.Sprintf("    Target root: %#x\n", att.Data.Target.Root))
			}
		}
	}

	return res.String(), nil
}

func outputBlockAttesterSlashings(ctx context.Context, eth2Client eth2client.Service, verbose bool, attesterSlashings []*phase0.AttesterSlashing) (string, error) {
	res := strings.Builder{}

	res.WriteString(fmt.Sprintf("Attester slashings: %d\n", len(attesterSlashings)))
	if verbose {
		for i, slashing := range attesterSlashings {
			// Say what was slashed.
			att1 := slashing.Attestation1
			att2 := slashing.Attestation2
			slashedIndices := intersection(att1.AttestingIndices, att2.AttestingIndices)
			if len(slashedIndices) == 0 {
				continue
			}

			res.WriteString(fmt.Sprintf("  %d:\n", i))
			res.WriteString(fmt.Sprintln("    Slashed validators:"))
			validators, err := eth2Client.(eth2client.ValidatorsProvider).Validators(ctx, "head", slashedIndices)
			if err != nil {
				return "", errors.Wrap(err, "failed to obtain beacon committees")
			}
			for k, v := range validators {
				res.WriteString(fmt.Sprintf("      %#x (%d)\n", v.Validator.PublicKey[:], k))
			}

			// Say what caused the slashing.
			if att1.Data.Target.Epoch == att2.Data.Target.Epoch {
				res.WriteString(fmt.Sprintf("    Double voted for same target epoch (%d):\n", att1.Data.Target.Epoch))
				if !bytes.Equal(att1.Data.Target.Root[:], att2.Data.Target.Root[:]) {
					res.WriteString(fmt.Sprintf("      Attestation 1 target epoch root: %#x\n", att1.Data.Target.Root))
					res.WriteString(fmt.Sprintf("      Attestation 2target epoch root: %#x\n", att2.Data.Target.Root))
				}
				if !bytes.Equal(att1.Data.BeaconBlockRoot[:], att2.Data.BeaconBlockRoot[:]) {
					res.WriteString(fmt.Sprintf("      Attestation 1 beacon block root: %#x\n", att1.Data.BeaconBlockRoot))
					res.WriteString(fmt.Sprintf("      Attestation 2 beacon block root: %#x\n", att2.Data.BeaconBlockRoot))
				}
			} else if att1.Data.Source.Epoch < att2.Data.Source.Epoch &&
				att1.Data.Target.Epoch > att2.Data.Target.Epoch {
				res.WriteString("    Surround voted:\n")
				res.WriteString(fmt.Sprintf("      Attestation 1 vote: %d->%d\n", att1.Data.Source.Epoch, att1.Data.Target.Epoch))
				res.WriteString(fmt.Sprintf("      Attestation 2 vote: %d->%d\n", att2.Data.Source.Epoch, att2.Data.Target.Epoch))
			}
		}
	}

	return res.String(), nil
}

func outputBlockDeposits(ctx context.Context, verbose bool, deposits []*phase0.Deposit) (string, error) {
	res := strings.Builder{}

	// Deposits.
	res.WriteString(fmt.Sprintf("Deposits: %d\n", len(deposits)))
	if verbose {
		for i, deposit := range deposits {
			data := deposit.Data
			res.WriteString(fmt.Sprintf("  %d:\n", i))
			res.WriteString(fmt.Sprintf("    Public key: %#x\n", data.PublicKey))
			res.WriteString(fmt.Sprintf("    Amount: %s\n", string2eth.GWeiToString(uint64(data.Amount), true)))
			res.WriteString(fmt.Sprintf("    Withdrawal credentials: %#x\n", data.WithdrawalCredentials))
			res.WriteString(fmt.Sprintf("    Signature: %#x\n", data.Signature))
		}
	}

	return res.String(), nil
}

func outputBlockVoluntaryExits(ctx context.Context, eth2Client eth2client.Service, verbose bool, voluntaryExits []*phase0.SignedVoluntaryExit) (string, error) {
	res := strings.Builder{}

	res.WriteString(fmt.Sprintf("Voluntary exits: %d\n", len(voluntaryExits)))
	if verbose {
		for i, voluntaryExit := range voluntaryExits {
			res.WriteString(fmt.Sprintf("  %d:\n", i))
			validators, err := eth2Client.(eth2client.ValidatorsProvider).Validators(ctx, "head", []phase0.ValidatorIndex{voluntaryExit.Message.ValidatorIndex})
			if err != nil {
				res.WriteString(fmt.Sprintf("  Error: failed to obtain validators: %v\n", err))
			} else {
				res.WriteString(fmt.Sprintf("    Validator: %#x (%d)\n", validators[0].Validator.PublicKey, voluntaryExit.Message.ValidatorIndex))
				res.WriteString(fmt.Sprintf("    Epoch: %d\n", voluntaryExit.Message.Epoch))
			}
		}
	}

	return res.String(), nil
}

func outputBlockSyncAggregate(ctx context.Context, eth2Client eth2client.Service, verbose bool, syncAggregate *altair.SyncAggregate, epoch phase0.Epoch) (string, error) {
	res := strings.Builder{}

	res.WriteString("Sync aggregate: ")
	res.WriteString(fmt.Sprintf("%d/%d\n", syncAggregate.SyncCommitteeBits.Count(), syncAggregate.SyncCommitteeBits.Len()))
	if verbose {
		specProvider, isProvider := eth2Client.(eth2client.SpecProvider)
		if isProvider {
			config, err := specProvider.Spec(ctx)
			if err == nil {
				slotsPerEpoch := config["SLOTS_PER_EPOCH"].(uint64)

				res.WriteString("  Contributions: ")
				res.WriteString(bitvectorToString(syncAggregate.SyncCommitteeBits))
				res.WriteString("\n")

				syncCommitteesProvider, isProvider := eth2Client.(eth2client.SyncCommitteesProvider)
				if isProvider {
					syncCommittee, err := syncCommitteesProvider.SyncCommittee(ctx, fmt.Sprintf("%d", uint64(epoch)*slotsPerEpoch))
					if err != nil {
						res.WriteString(fmt.Sprintf("  Error: failed to obtain sync committee: %v\n", err))
					} else {
						res.WriteString("  Contributing validators:")
						for i := uint64(0); i < syncAggregate.SyncCommitteeBits.Len(); i++ {
							if syncAggregate.SyncCommitteeBits.BitAt(i) {
								res.WriteString(fmt.Sprintf(" %d", syncCommittee.Validators[i]))
							}
						}
						res.WriteString("\n")
					}
				}
			}
		}
	}

	return res.String(), nil
}

func outputAltairBlockText(ctx context.Context, data *dataOut, signedBlock *altair.SignedBeaconBlock) (string, error) {
	if signedBlock == nil {
		return "", errors.New("no block supplied")
	}

	body := signedBlock.Message.Body

	res := strings.Builder{}

	// General info.
	blockRoot, err := signedBlock.Message.HashTreeRoot()
	if err != nil {
		return "", errors.Wrap(err, "failed to obtain block root")
	}
	bodyRoot, err := signedBlock.Message.Body.HashTreeRoot()
	if err != nil {
		return "", errors.Wrap(err, "failed to generate body root")
	}

	tmp, err := outputBlockGeneral(ctx,
		data.verbose,
		signedBlock.Message.Slot,
		blockRoot,
		bodyRoot,
		signedBlock.Message.ParentRoot,
		signedBlock.Message.StateRoot,
		signedBlock.Message.Body.Graffiti,
		data.genesisTime,
		data.slotDuration,
		data.slotsPerEpoch)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	// Eth1 data.
	if data.verbose {
		tmp, err := outputBlockETH1Data(ctx, body.ETH1Data)
		if err != nil {
			return "", err
		}
		res.WriteString(tmp)
	}

	// Sync aggregate.
	tmp, err = outputBlockSyncAggregate(ctx, data.eth2Client, data.verbose, signedBlock.Message.Body.SyncAggregate, phase0.Epoch(uint64(signedBlock.Message.Slot)/data.slotsPerEpoch))
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	// Attestations.
	tmp, err = outputBlockAttestations(ctx, data.eth2Client, data.verbose, signedBlock.Message.Body.Attestations)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	// Attester slashings.
	tmp, err = outputBlockAttesterSlashings(ctx, data.eth2Client, data.verbose, signedBlock.Message.Body.AttesterSlashings)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	res.WriteString(fmt.Sprintf("Proposer slashings: %d\n", len(body.ProposerSlashings)))
	// Add verbose proposer slashings.

	tmp, err = outputBlockDeposits(ctx, data.verbose, signedBlock.Message.Body.Deposits)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	// Voluntary exits.
	tmp, err = outputBlockVoluntaryExits(ctx, data.eth2Client, data.verbose, signedBlock.Message.Body.VoluntaryExits)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	return res.String(), nil
}

func outputPhase0BlockText(ctx context.Context, data *dataOut, signedBlock *phase0.SignedBeaconBlock) (string, error) {
	if signedBlock == nil {
		return "", errors.New("no block supplied")
	}

	body := signedBlock.Message.Body

	res := strings.Builder{}

	// General info.
	blockRoot, err := signedBlock.Message.HashTreeRoot()
	if err != nil {
		return "", errors.Wrap(err, "failed to obtain block root")
	}
	bodyRoot, err := signedBlock.Message.Body.HashTreeRoot()
	if err != nil {
		return "", errors.Wrap(err, "failed to generate block root")
	}
	tmp, err := outputBlockGeneral(ctx,
		data.verbose,
		signedBlock.Message.Slot,
		blockRoot,
		bodyRoot,
		signedBlock.Message.ParentRoot,
		signedBlock.Message.StateRoot,
		signedBlock.Message.Body.Graffiti,
		data.genesisTime,
		data.slotDuration,
		data.slotsPerEpoch)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	// Eth1 data.
	if data.verbose {
		tmp, err := outputBlockETH1Data(ctx, body.ETH1Data)
		if err != nil {
			return "", err
		}
		res.WriteString(tmp)
	}

	// Attestations.
	tmp, err = outputBlockAttestations(ctx, data.eth2Client, data.verbose, signedBlock.Message.Body.Attestations)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	// Attester slashings.
	tmp, err = outputBlockAttesterSlashings(ctx, data.eth2Client, data.verbose, signedBlock.Message.Body.AttesterSlashings)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	res.WriteString(fmt.Sprintf("Proposer slashings: %d\n", len(body.ProposerSlashings)))
	// Add verbose proposer slashings.

	tmp, err = outputBlockDeposits(ctx, data.verbose, signedBlock.Message.Body.Deposits)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	// Voluntary exits.
	tmp, err = outputBlockVoluntaryExits(ctx, data.eth2Client, data.verbose, signedBlock.Message.Body.VoluntaryExits)
	if err != nil {
		return "", err
	}
	res.WriteString(tmp)

	return res.String(), nil
}

// intersection returns a list of items common between the two sets.
func intersection(set1 []uint64, set2 []uint64) []phase0.ValidatorIndex {
	sort.Slice(set1, func(i, j int) bool { return set1[i] < set1[j] })
	sort.Slice(set2, func(i, j int) bool { return set2[i] < set2[j] })
	res := make([]phase0.ValidatorIndex, 0)

	set1Pos := 0
	set2Pos := 0
	for set1Pos < len(set1) && set2Pos < len(set2) {
		switch {
		case set1[set1Pos] < set2[set2Pos]:
			set1Pos++
		case set2[set2Pos] < set1[set1Pos]:
			set2Pos++
		default:
			res = append(res, phase0.ValidatorIndex(set1[set1Pos]))
			set1Pos++
			set2Pos++
		}
	}

	return res
}

func bitlistToString(input bitfield.Bitlist) string {
	bits := int(input.Len())

	res := ""
	for i := 0; i < bits; i++ {
		if input.BitAt(uint64(i)) {
			res = fmt.Sprintf("%s✓", res)
		} else {
			res = fmt.Sprintf("%s✕", res)
		}
		if i%8 == 7 {
			res = fmt.Sprintf("%s ", res)
		}
	}
	return strings.TrimSpace(res)
}

func bitvectorToString(input bitfield.Bitvector512) string {
	bits := int(input.Len())

	res := strings.Builder{}
	for i := 0; i < bits; i++ {
		if input.BitAt(uint64(i)) {
			res.WriteString("✓")
		} else {
			res.WriteString("✕")
		}
		if i%8 == 7 && i != bits-1 {
			res.WriteString(" ")
		}
	}
	return res.String()
}

func attestingIndices(input bitfield.Bitlist, indices []phase0.ValidatorIndex) string {
	bits := int(input.Len())
	res := ""
	for i := 0; i < bits; i++ {
		if input.BitAt(uint64(i)) {
			res = fmt.Sprintf("%s%d ", res, indices[i])
		}
	}
	return strings.TrimSpace(res)
}
