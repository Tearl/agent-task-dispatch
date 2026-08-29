package taskfunding

import (
	"database/sql"
	"testing"
)

func TestFundingConfigurationRejectsUnsupportedChains(t *testing.T) {
	db := &sql.DB{}
	contract := "0x0000000000000000000000000000000000001234"
	asset := "0x0000000000000000000000000000000000005678"
	for _, chainID := range []string{"1", "11155111", "0", "-1"} {
		if _, err := NewService(db, Config{ChainID: chainID, ContractAddress: contract, AssetAddress: asset, Asset: "evm:" + chainID + "/erc20:" + asset}); err == nil {
			t.Fatalf("unsupported chain %s was accepted", chainID)
		}
	}
	for _, chainID := range []string{"31337", "84532"} {
		if _, err := NewService(db, Config{ChainID: chainID, ContractAddress: contract, AssetAddress: asset, Asset: "evm:" + chainID + "/erc20:" + asset}); err != nil {
			t.Fatalf("supported chain %s was rejected: %v", chainID, err)
		}
	}
}
