package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/bandprotocol/bandchain/chain/pkg/filecache"
	codecstd "github.com/cosmos/cosmos-sdk/codec/std"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tendermint/go-amino"
	"github.com/tendermint/tendermint/libs/cli"

	"github.com/bandprotocol/bandchain/chain/x/oracle"
	otypes "github.com/bandprotocol/bandchain/chain/x/oracle/types"
)

// AddGenesisDataSourceCmd returns add-data-source cobra Command.
func AddGenesisDataSourceCmd(
	ctx *server.Context, depCdc *amino.Codec, cdc *codecstd.Codec, defaultNodeHome string,
) *cobra.Command {

	cmd := &cobra.Command{
		Use:   "add-data-source [name] [description] [owner] [filepath]",
		Short: "Add a data source to genesis.json",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			config := ctx.Config
			config.SetRoot(viper.GetString(cli.HomeFlag))
			f := filecache.New(filepath.Join(viper.GetString(cli.HomeFlag), "files"))
			data, err := ioutil.ReadFile(args[3])
			if err != nil {
				return err
			}
			filename := f.AddFile(data)
			owner, err := sdk.AccAddressFromBech32(args[2])
			if err != nil {
				return err
			}

			genFile := config.GenesisFile()
			appState, genDoc, err := genutil.GenesisStateFromGenFile(depCdc, genFile)
			if err != nil {
				return fmt.Errorf("failed to unmarshal genesis state: %w", err)
			}

			oracleGenState := oracle.GetGenesisStateFromAppState(depCdc, appState)
			oracleGenState.DataSources = append(oracleGenState.DataSources, otypes.NewDataSource(
				owner, args[0], args[1], filename,
			))

			appState[oracle.ModuleName] = cdc.MustMarshalJSON(oracleGenState)
			appStateJSON := cdc.MustMarshalJSON(appState)
			genDoc.AppState = appStateJSON
			return genutil.ExportGenesisFile(genDoc, genFile)
		},
	}

	cmd.Flags().String(cli.HomeFlag, defaultNodeHome, "node's home directory")
	return cmd
}
