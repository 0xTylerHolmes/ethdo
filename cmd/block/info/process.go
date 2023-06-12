// Copyright © 2019 - 2023 Weald Technology Trading.
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
	"context"
	"encoding/json"
	"fmt"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

var (
	jsonOutput bool
	sszOutput  bool
	results    *dataOut
)

func process(ctx context.Context, data *dataIn) (*dataOut, error) {
	if data == nil {
		return nil, errors.New("no data")
	}
	if data.blockID == "" {
		return nil, errors.New("no block ID")
	}

	results = &dataOut{
		debug:      data.debug,
		verbose:    data.verbose,
		eth2Client: data.eth2Client,
	}

	config, err := results.eth2Client.(eth2client.SpecProvider).Spec(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to obtain configuration information")
	}
	genesis, err := results.eth2Client.(eth2client.GenesisProvider).Genesis(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to obtain genesis information")
	}
	results.genesisTime = genesis.GenesisTime
	results.slotDuration = config["SECONDS_PER_SLOT"].(time.Duration)
	results.slotsPerEpoch = config["SLOTS_PER_EPOCH"].(uint64)

	signedBlock, err := results.eth2Client.(eth2client.SignedBeaconBlockProvider).SignedBeaconBlock(ctx, data.blockID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to obtain beacon block")
	}
	if signedBlock == nil {
		return nil, errors.New("empty beacon block")
	}

	switch signedBlock.Version {
	case spec.DataVersionPhase0:
		if err := outputPhase0Block(ctx, data.jsonOutput, signedBlock.Phase0); err != nil {
			return nil, errors.Wrap(err, "failed to output block")
		}
	case spec.DataVersionAltair:
		if err := outputAltairBlock(ctx, data.jsonOutput, data.sszOutput, signedBlock.Altair); err != nil {
			return nil, errors.Wrap(err, "failed to output block")
		}
	case spec.DataVersionBellatrix:
		if err := outputBellatrixBlock(ctx, data.jsonOutput, data.sszOutput, signedBlock.Bellatrix); err != nil {
			return nil, errors.Wrap(err, "failed to output block")
		}
	case spec.DataVersionCapella:
		if err := outputCapellaBlock(ctx, data.jsonOutput, data.sszOutput, signedBlock.Capella); err != nil {
			return nil, errors.Wrap(err, "failed to output block")
		}
	case spec.DataVersionDeneb:
		blobs, err := results.eth2Client.(eth2client.BeaconBlockBlobsProvider).BeaconBlockBlobs(ctx, data.blockID)
		if err != nil {
			return nil, errors.Wrap(err, "failed to obtain blobs")
		}
		if err := outputDenebBlock(ctx, data.jsonOutput, data.sszOutput, signedBlock.Deneb, blobs); err != nil {
			return nil, errors.Wrap(err, "failed to output block")
		}
	default:
		return nil, errors.New("unknown block version")
	}

	if data.stream {
		jsonOutput = data.jsonOutput
		sszOutput = data.sszOutput
		if !jsonOutput && !sszOutput {
			fmt.Println("")
		}
		err := data.eth2Client.(eth2client.EventsProvider).Events(ctx, []string{"head"}, headEventHandler)
		if err != nil {
			return nil, errors.Wrap(err, "failed to start block stream")
		}
		<-ctx.Done()
	}

	return &dataOut{}, nil
}

func headEventHandler(event *api.Event) {
	ctx := context.Background()

	// Only interested in head events.
	if event.Topic != "head" {
		return
	}

	blockID := fmt.Sprintf("%#x", event.Data.(*api.HeadEvent).Block[:])
	signedBlock, err := results.eth2Client.(eth2client.SignedBeaconBlockProvider).SignedBeaconBlock(ctx, blockID)
	if err != nil {
		if !jsonOutput && !sszOutput {
			fmt.Printf("Failed to obtain block: %v\n", err)
		}
		return
	}
	if signedBlock == nil {
		if !jsonOutput && !sszOutput {
			fmt.Println("Empty beacon block")
		}
		return
	}

	switch signedBlock.Version {
	case spec.DataVersionPhase0:
		err = outputPhase0Block(ctx, jsonOutput, signedBlock.Phase0)
	case spec.DataVersionAltair:
		err = outputAltairBlock(ctx, jsonOutput, sszOutput, signedBlock.Altair)
	case spec.DataVersionBellatrix:
		err = outputBellatrixBlock(ctx, jsonOutput, sszOutput, signedBlock.Bellatrix)
	case spec.DataVersionCapella:
		err = outputCapellaBlock(ctx, jsonOutput, sszOutput, signedBlock.Capella)
	case spec.DataVersionDeneb:
		var blobs []*deneb.BlobSidecar
		blobs, err = results.eth2Client.(eth2client.BeaconBlockBlobsProvider).BeaconBlockBlobs(ctx, blockID)
		if err == nil {
			err = outputDenebBlock(context.Background(), jsonOutput, sszOutput, signedBlock.Deneb, blobs)
		}
	default:
		err = errors.New("unknown block version")
	}
	if err != nil && !jsonOutput && !sszOutput {
		fmt.Printf("Failed to output block: %v\n", err)
		return
	}

	if !jsonOutput && !sszOutput {
		fmt.Println("")
	}
}

func outputPhase0Block(ctx context.Context, jsonOutput bool, signedBlock *phase0.SignedBeaconBlock) error {
	switch {
	case jsonOutput:
		data, err := json.Marshal(signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate JSON")
		}
		fmt.Printf("%s\n", string(data))
	default:
		data, err := outputPhase0BlockText(ctx, results, signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate text")
		}
		fmt.Print(data)
	}
	return nil
}

func outputAltairBlock(ctx context.Context, jsonOutput bool, sszOutput bool, signedBlock *altair.SignedBeaconBlock) error {
	switch {
	case jsonOutput:
		data, err := json.Marshal(signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate JSON")
		}
		fmt.Printf("%s\n", string(data))
	case sszOutput:
		data, err := signedBlock.MarshalSSZ()
		if err != nil {
			return errors.Wrap(err, "failed to generate SSZ")
		}
		fmt.Printf("%x\n", data)
	default:
		data, err := outputAltairBlockText(ctx, results, signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate text")
		}
		fmt.Print(data)
	}
	return nil
}

func outputBellatrixBlock(ctx context.Context, jsonOutput bool, sszOutput bool, signedBlock *bellatrix.SignedBeaconBlock) error {
	switch {
	case jsonOutput:
		data, err := json.Marshal(signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate JSON")
		}
		fmt.Printf("%s\n", string(data))
	case sszOutput:
		data, err := signedBlock.MarshalSSZ()
		if err != nil {
			return errors.Wrap(err, "failed to generate SSZ")
		}
		fmt.Printf("%x\n", data)
	default:
		data, err := outputBellatrixBlockText(ctx, results, signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate text")
		}
		fmt.Print(data)
	}
	return nil
}

func outputCapellaBlock(ctx context.Context, jsonOutput bool, sszOutput bool, signedBlock *capella.SignedBeaconBlock) error {
	switch {
	case jsonOutput:
		data, err := json.Marshal(signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate JSON")
		}
		fmt.Printf("%s\n", string(data))
	case sszOutput:
		data, err := signedBlock.MarshalSSZ()
		if err != nil {
			return errors.Wrap(err, "failed to generate SSZ")
		}
		fmt.Printf("%x\n", data)
	default:
		data, err := outputCapellaBlockText(ctx, results, signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate text")
		}
		fmt.Print(data)
	}
	return nil
}

func outputDenebBlock(ctx context.Context,
	jsonOutput bool,
	sszOutput bool,
	signedBlock *deneb.SignedBeaconBlock,
	blobs []*deneb.BlobSidecar,
) error {
	switch {
	case jsonOutput:
		data, err := json.Marshal(signedBlock)
		if err != nil {
			return errors.Wrap(err, "failed to generate JSON")
		}
		fmt.Printf("%s\n", string(data))
	case sszOutput:
		data, err := signedBlock.MarshalSSZ()
		if err != nil {
			return errors.Wrap(err, "failed to generate SSZ")
		}
		fmt.Printf("%x\n", data)
	default:
		data, err := outputDenebBlockText(ctx, results, signedBlock, blobs)
		if err != nil {
			return errors.Wrap(err, "failed to generate text")
		}
		fmt.Print(data)
	}
	return nil
}
