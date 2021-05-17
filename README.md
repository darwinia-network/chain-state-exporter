# Chain State Exporter

![](https://img.shields.io/github/workflow/status/darwinia-network/chain-state-exporter/Production)
![](https://img.shields.io/github/v/release/darwinia-network/chain-state-exporter)

Chain State Exporter collects the state of Darwinia Network and exposes them in Prometheus compatible metrics formats.

Currently, it works with Darwinia Mainnet and Darwinia Crab.

## Releases

Until `v1` released, chain-state-exporter is still under development, any API or CLI options may be changed in future versions.

The releases are under [GitHub's release page](https://github.com/darwinia-network/chain-state-exporter/releases). You can pull the image by using one of the versions, for example:

```bash
docker pull quay.io/darwinia-network/chain-state-exporter:v0.1.0
```

## Usage

1. Download custom types definition file:

    ```bash
    wget -O types.json https://raw.githubusercontent.com/darwinia-network/chain-state-exporter/master/types.json
    ```

2. Run the exporter with `types.json` and the official Darwinia RPC node (`wss://rpc.darwinia.network`):

    ```bash
    docker run -d \
        -p 9602:9602 \
        -v $PWD/types.json:/types.json \
        quay.io/darwinia-network/chain-state-exporter:v0.1.0 \
            --ws-endpoint wss://rpc.darwinia.network
    ```

    `9602` is the default port of chain-state-exporter. Don't forget to check out the latest release instead of using the version `v0.1.0` which might be stale already.

3. Test the exporter with curl command:

    ```bash
    curl localhost:9602/metrics
    ```

    Example results:

    ```
    # HELP darwinia_state_active_era_index
    # TYPE darwinia_state_active_era_index counter
    darwinia_state_active_era_index 97

    ...

    # HELP darwinia_state_validators_total
    # TYPE darwinia_state_validators_total gauge
    darwinia_state_validators_total 69
    ```

4. Example Prometheus config with static target:

```yaml
scrape_configs:
  - job_name:
    static_configs:
      - targets:
          - 127.0.0.1:9162
```

## Metrics Reference

All metrics exposed by chain-state-exporter are prefixed with `darwinia_state_`.

New metrics proposals are always welcome, please feel free to submit an issue.

### Meta Metrics

| Metric Name                                 | Description                                                                          |
| ------------------------------------------- | ------------------------------------------------------------------------------------ |
| darwinia_state_last_scrape_error            | Whether the last scrape of metrics resulted in an error (1 for error, 0 for success) |
| darwinia_state_last_scrape_duration_seconds | Time duration of last scrape in seconds                                              |

### Storage Metrics

| Metric Name                                         | Source (Chain Storage Name)                 | Description                                                                                                                 |
| --------------------------------------------------- | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| darwinia_state_active_era_index                     | staking.activeEra                           |                                                                                                                             |
| darwinia_state_session_index                        | session.currentIndex                        |                                                                                                                             |
| darwinia_state_validators_total                     | session.validators                          | Size of current validators set                                                                                              |
| darwinia_state_era_reward_points                    | staking.erasRewardPoints                    | Label `account_id` is the public key of corresponding validator,<br>label `address` is the SS58 address on Darwinia network |
| darwinia_state_pending_headers_total                | ethereumRelay.pendingRelayHeaderParcels     | Number of pending Ethereum header parcels                                                                                   |
| darwinia_state_mmr_roots_to_sign_total              | ethereumRelayAuthorities.mMRRootsToSignKeys | Number of MMR roots that need to be signed                                                                                  |
| darwinia_state_authorities_to_sign                  | ethereumRelayAuthorities.authoritiesToSign  | Whether there is a new authority set that need to be signed, can be either `1` or `0`                                       |
| darwinia_state_authorities_to_sign_votes            | ethereumRelayAuthorities.authoritiesToSign  | Number of signed votes of ongoing authorities change request, `0` if darwinia_state_authorities_to_sign == 0                |
| darwinia_state_authorities_to_sign_deadline         | ethereumRelayAuthorities.nextAuthorities    | Block number deadline of ongoing authorities change request, `0` if darwinia_state_authorities_to_sign == 0                 |
| darwinia_state_best_confirmed_ethereum_block_number | ethereumRelay.bestConfirmedBlockNumber      |                                                                                                                             |
| darwinia_state_pending_header_ethereum_block_number | ethereumRelay.pendingRelayHeaderParcels     | Ethereum block number of every pending relay header parcel,<br>label `block_number` is the Darwinia block number            |

## License

MIT
