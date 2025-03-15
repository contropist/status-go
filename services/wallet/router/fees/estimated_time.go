package fees

import (
	"context"
	"math"
	"math/big"
	"sort"

	"github.com/status-im/status-go/services/wallet/common"
)

const (
	inclusionThreshold = 0.95

	priorityFeePercentileHigh   = 0.6
	priorityFeePercentileMedium = 0.5
	priorityFeePercentileLow    = 0.4

	baseFeePercentileFirstBlock  = 0.8
	baseFeePercentileSecondBlock = 0.7
	baseFeePercentileThirdBlock  = 0.6
	baseFeePercentileFourthBlock = 0.5
	baseFeePercentileFifthBlock  = 0.4
	baseFeePercentileSixthBlock  = 0.3
)

type TransactionEstimation int

const (
	Unknown TransactionEstimation = iota
	LessThanOneMinute
	LessThanThreeMinutes
	LessThanFiveMinutes
	MoreThanFiveMinutes
)

func (f *FeeManager) TransactionEstimatedTime(ctx context.Context, chainID uint64, maxFeePerGas *big.Int) TransactionEstimation {
	feeHistory, err := f.getFeeHistory(ctx, chainID, 100, "latest", nil)
	if err != nil {
		return Unknown
	}

	return f.estimatedTime(feeHistory, maxFeePerGas)
}

func (f *FeeManager) estimatedTime(feeHistory *FeeHistory, maxFeePerGas *big.Int) TransactionEstimation {
	fees := f.convertToBigIntAndSort(feeHistory.BaseFeePerGas)
	if len(fees) == 0 {
		return Unknown
	}

	// pEvent represents the probability of the transaction being included in a block,
	// we assume this one is static over time, in reality it is not.
	pEvent := 0.0
	for idx, fee := range fees {
		if fee.Cmp(maxFeePerGas) == 1 || idx == len(fees)-1 {
			pEvent = float64(idx) / float64(len(fees))
			break
		}
	}

	// Probability of next 4 blocks including the transaction (less than 1 minute)
	// Generalising the formula: P(AUB) = P(A) + P(B) - P(A∩B) for 4 events and in our context P(A) == P(B) == pEvent
	// The factors are calculated using the combinations formula
	probability := pEvent*4 - 6*(math.Pow(pEvent, 2)) + 4*(math.Pow(pEvent, 3)) - (math.Pow(pEvent, 4))
	if probability >= inclusionThreshold {
		return LessThanOneMinute
	}

	// Probability of next 12 blocks including the transaction (less than 5 minutes)
	// Generalising the formula: P(AUB) = P(A) + P(B) - P(A∩B) for 20 events and in our context P(A) == P(B) == pEvent
	// The factors are calculated using the combinations formula
	probability = pEvent*12 -
		66*(math.Pow(pEvent, 2)) +
		220*(math.Pow(pEvent, 3)) -
		495*(math.Pow(pEvent, 4)) +
		792*(math.Pow(pEvent, 5)) -
		924*(math.Pow(pEvent, 6)) +
		792*(math.Pow(pEvent, 7)) -
		495*(math.Pow(pEvent, 8)) +
		220*(math.Pow(pEvent, 9)) -
		66*(math.Pow(pEvent, 10)) +
		12*(math.Pow(pEvent, 11)) -
		math.Pow(pEvent, 12)
	if probability >= inclusionThreshold {
		return LessThanThreeMinutes
	}

	// Probability of next 20 blocks including the transaction (less than 5 minutes)
	// Generalising the formula: P(AUB) = P(A) + P(B) - P(A∩B) for 20 events and in our context P(A) == P(B) == pEvent
	// The factors are calculated using the combinations formula
	probability = pEvent*20 -
		190*(math.Pow(pEvent, 2)) +
		1140*(math.Pow(pEvent, 3)) -
		4845*(math.Pow(pEvent, 4)) +
		15504*(math.Pow(pEvent, 5)) -
		38760*(math.Pow(pEvent, 6)) +
		77520*(math.Pow(pEvent, 7)) -
		125970*(math.Pow(pEvent, 8)) +
		167960*(math.Pow(pEvent, 9)) -
		184756*(math.Pow(pEvent, 10)) +
		167960*(math.Pow(pEvent, 11)) -
		125970*(math.Pow(pEvent, 12)) +
		77520*(math.Pow(pEvent, 13)) -
		38760*(math.Pow(pEvent, 14)) +
		15504*(math.Pow(pEvent, 15)) -
		4845*(math.Pow(pEvent, 16)) +
		1140*(math.Pow(pEvent, 17)) -
		190*(math.Pow(pEvent, 18)) +
		20*(math.Pow(pEvent, 19)) -
		math.Pow(pEvent, 20)
	if probability >= inclusionThreshold {
		return LessThanFiveMinutes
	}

	return MoreThanFiveMinutes
}

func (f *FeeManager) convertToBigIntAndSort(hexArray []string) []*big.Int {
	values := []*big.Int{}
	for _, sValue := range hexArray {
		iValue := new(big.Int)
		_, ok := iValue.SetString(sValue, 0)
		if !ok {
			continue
		}
		values = append(values, iValue)
	}

	sort.Slice(values, func(i, j int) bool { return values[i].Cmp(values[j]) < 0 })
	return values
}

func (f *FeeManager) getFeeHistoryForTimeEstimation(ctx context.Context, chainID uint64) (*FeeHistory, error) {
	blockCount := uint64(10) // use the last 10 blocks for L1 chains
	if chainID != common.EthereumMainnet &&
		chainID != common.EthereumSepolia &&
		chainID != common.AnvilMainnet {
		blockCount = 50 // use the last 50 blocks for L2 chains
	}
	return f.getFeeHistory(ctx, chainID, blockCount, "latest", []int{RewardPercentiles2})
}

func calculateTimeForInclusion(chainID uint64, expectedInclusionInBlock int) uint {
	blockCreationTime := common.GetBlockCreationTimeForChain(chainID)
	blockCreationTimeInSeconds := uint(blockCreationTime.Seconds())

	// the client will decide how to display estimated times, status-go sends it in the steps of 5 (for example the client
	// should display "more than 1 minute" if the expected inclusion time is 60 seconds or more.
	expectedInclusionTime := uint(expectedInclusionInBlock) * blockCreationTimeInSeconds
	return (expectedInclusionTime/5 + 1) * 5
}

func getBaseFeePercentileIndex(sortedBaseFees []*big.Int, percentile float64, networkCongestion float64) int {
	// calculate the index of the base fee for the given percentile corrected by the network congestion
	index := int(float64(len(sortedBaseFees)) * percentile * networkCongestion)
	if index >= len(sortedBaseFees) {
		return len(sortedBaseFees) - 1
	}
	return index
}

// TransactionEstimatedTimeV2 returns the estimated time in seconds for a transaction to be included in a block
func (f *FeeManager) TransactionEstimatedTimeV2(ctx context.Context, chainID uint64, maxFeePerGas *big.Int, priorityFee *big.Int) uint {
	feeHistory, err := f.getFeeHistoryForTimeEstimation(ctx, chainID)
	if err != nil {
		return 0
	}

	return f.estimatedTimeV2(feeHistory, maxFeePerGas, priorityFee, chainID)
}

func (f *FeeManager) estimatedTimeV2(feeHistory *FeeHistory, txMaxFeePerGas *big.Int, txPriorityFee *big.Int, chainID uint64) uint {
	sortedBaseFees := f.convertToBigIntAndSort(feeHistory.BaseFeePerGas)
	if len(sortedBaseFees) == 0 {
		return 0
	}

	var mediumPriorityFees []string // based on 50th percentile in the last 100 blocks
	for _, fee := range feeHistory.Reward {
		mediumPriorityFees = append(mediumPriorityFees, fee[0])
	}
	mediumPriorityFeesSorted := f.convertToBigIntAndSort(mediumPriorityFees)
	if len(mediumPriorityFeesSorted) == 0 {
		return 0
	}

	txBaseFee := new(big.Int).Sub(txMaxFeePerGas, txPriorityFee)

	networkCongestion := calculateNetworkCongestion(feeHistory)

	// Priority fee for the first two blocks has to be higher than 60th percentile of the mediumPriorityFeesSorted
	priorityFeePercentileIndex := int(float64(len(mediumPriorityFeesSorted)) * priorityFeePercentileHigh)
	priorityFeeForFirstTwoBlock := mediumPriorityFeesSorted[priorityFeePercentileIndex]
	// Priority fee for the second two blocks has to be higher than 50th percentile of the mediumPriorityFeesSorted
	priorityFeePercentileIndex = int(float64(len(mediumPriorityFeesSorted)) * priorityFeePercentileMedium)
	priorityFeeForSecondTwoBlocks := mediumPriorityFeesSorted[priorityFeePercentileIndex]
	// Priority fee for the third two blocks has to be higher than 40th percentile of the mediumPriorityFeesSorted
	priorityFeePercentileIndex = int(float64(len(mediumPriorityFeesSorted)) * priorityFeePercentileLow)
	priorityFeeForThirdTwoBlocks := mediumPriorityFeesSorted[priorityFeePercentileIndex]

	// To include the transaction in the block `inclusionInBlock` its base fee has to be in a higher than `baseFeePercentile`
	// and its priority fee has to be higher than the `priorityFee`
	inclusions := []struct {
		inclusionInBlock  int
		baseFeePercentile float64
		priorityFee       *big.Int
	}{
		{1, baseFeePercentileFirstBlock, priorityFeeForFirstTwoBlock},
		{2, baseFeePercentileSecondBlock, priorityFeeForFirstTwoBlock},
		{3, baseFeePercentileThirdBlock, priorityFeeForSecondTwoBlocks},
		{4, baseFeePercentileFourthBlock, priorityFeeForSecondTwoBlocks},
		{5, baseFeePercentileFifthBlock, priorityFeeForThirdTwoBlocks},
		{6, baseFeePercentileSixthBlock, priorityFeeForThirdTwoBlocks},
	}

	for _, p := range inclusions {
		baseFeePercentileIndex := getBaseFeePercentileIndex(sortedBaseFees, p.baseFeePercentile, networkCongestion)
		if txBaseFee.Cmp(sortedBaseFees[baseFeePercentileIndex]) >= 0 && txPriorityFee.Cmp(p.priorityFee) >= 0 {
			return calculateTimeForInclusion(chainID, p.inclusionInBlock)
		}
	}

	return 0
}
