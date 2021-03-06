/*
 * Copyright 2020 Vectorized, Inc.
 *
 * Use of this software is governed by the Business Source License
 * included in the file licenses/BSL.md
 *
 * As of the Change Date specified in that file, in accordance with
 * the Business Source License, use of this software will be governed
 * by the Apache License, Version 2.0
 */

#pragma once

#include "model/fundamental.h"
#include "raft/types.h"
#include "seastarx.h"

#include <seastar/core/reactor.hh> // shard_id

#include <absl/container/flat_hash_map.h>

namespace cluster {
/// \brief this is populated by consensus::controller
/// every core will have a _full_ copy of all indexes
class shard_table final {
public:
    bool contains(const raft::group_id& group) {
        return _group_idx.find(group) != _group_idx.end();
    }
    ss::shard_id shard_for(const raft::group_id& group) {
        return _group_idx.find(group)->second;
    }

    /**
     * \brief Lookup the owning shard for an ntp.
     */
    std::optional<ss::shard_id> shard_for(const model::ntp& ntp) {
        if (auto it = _ntp_idx.find(ntp); it != _ntp_idx.end()) {
            return it->second;
        }
        return std::nullopt;
    }
    void insert(model::ntp ntp, ss::shard_id i) {
        _ntp_idx.insert({std::move(ntp), i});
    }
    void insert(raft::group_id g, ss::shard_id i) { _group_idx.insert({g, i}); }

    void erase(const model::ntp& ntp, raft::group_id g) {
        _ntp_idx.erase(ntp);
        _group_idx.erase(g);
    }

private:
    // kafka index
    absl::flat_hash_map<model::ntp, ss::shard_id> _ntp_idx;
    // raft index
    absl::flat_hash_map<raft::group_id, ss::shard_id> _group_idx;
};
} // namespace cluster
