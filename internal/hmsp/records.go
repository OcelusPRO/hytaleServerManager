package hmsp

import "fmt"

// Record kinds.
const (
	KindSnapshot = "snapshot"
	KindEvent    = "event"
	KindUnknown  = "unknown"
)

// Record is one decoded record. Data holds the type-specific fields with the
// names used by the reference receiver (snake_case), ready for JSON.
type Record struct {
	Type string `json:"type"`
	Kind string `json:"kind"`
	// TimeMs is the emission time of a snapshot, or the instant of an event
	// (datagram time minus the event age), in epoch milliseconds.
	TimeMs int64          `json:"timeMs"`
	Data   map[string]any `json:"data,omitempty"`
}

type decodeFunc func(r *reader) map[string]any

type recordSpec struct {
	name   string
	decode decodeFunc
}

var serverStates = []string{"BOOTING", "RUNNING", "SHUTTING_DOWN"}

var permissionOperations = map[uint8]string{
	1: "PLAYER_PERMISSIONS_ADDED", 2: "PLAYER_PERMISSIONS_REMOVED",
	3: "PLAYER_GROUP_ADDED", 4: "PLAYER_GROUP_REMOVED",
	5: "GROUP_PERMISSIONS_ADDED", 6: "GROUP_PERMISSIONS_REMOVED",
}

// ---- composites ------------------------------------------------------------

func actor(r *reader) map[string]any {
	switch r.u8() {
	case 1:
		return map[string]any{"kind": "player", "uuid": r.uuid(), "name": r.str()}
	case 2:
		return map[string]any{"kind": "entity", "uuid": r.uuid(), "type": r.str()}
	case 3:
		return map[string]any{"kind": "environment", "label": r.str()}
	}
	return map[string]any{"kind": "unknown"}
}

func pos(r *reader) map[string]any {
	return map[string]any{"x": r.svar(), "y": r.svar(), "z": r.svar()}
}

func vital(r *reader) map[string]any {
	return map[string]any{"value": r.f32(), "max": r.f32()}
}

func listOf(r *reader, item func(r *reader) any) []any {
	n := r.count()
	out := make([]any, 0, n)
	for i := 0; i < n && r.err() == nil; i++ {
		out = append(out, item(r))
	}
	return out
}

func playerIdent(r *reader) map[string]any {
	return map[string]any{"uuid": r.uuid(), "name": r.str()}
}

// withWorld reads a player identity followed by a world name.
func withWorld(r *reader) map[string]any {
	m := playerIdent(r)
	m["world"] = r.str()
	return m
}

// ---- snapshots -------------------------------------------------------------

func hello(r *reader) map[string]any {
	return map[string]any{
		"protocol": r.uvar(), "plugin_version": r.str(), "server_name": r.str(), "motd": r.str(),
		"max_players": r.svar(), "boot_epoch_ms": r.u64(), "stats_interval_ms": r.uvar(),
	}
}

func server(r *reader) map[string]any {
	out := map[string]any{
		"uptime_ms": r.uvar(), "online_players": r.uvar(), "max_players": r.svar(),
		"worlds": r.uvar(), "plugins": r.uvar(), "whitelist": r.bool(),
	}
	state := int(r.u8())
	if state < len(serverStates) {
		out["state"] = serverStates[state]
	} else {
		out["state"] = fmt.Sprintf("UNKNOWN_%d", state)
	}
	return out
}

func jvm(r *reader) map[string]any {
	out := map[string]any{
		"heap_used": r.uvar(), "heap_committed": r.uvar(), "heap_max": r.svar(),
		"non_heap_used": r.uvar(), "non_heap_committed": r.uvar(),
		"threads": r.uvar(), "daemon_threads": r.uvar(), "peak_threads": r.uvar(),
		"process_cpu": r.f32(), "system_cpu": r.f32(), "load_average": r.f32(),
		"processors": r.uvar(), "total_memory": r.uvar(), "free_memory": r.uvar(),
		"loaded_classes": r.uvar(), "uptime_ms": r.uvar(),
	}
	out["gc"] = listOf(r, func(r *reader) any {
		return map[string]any{"name": r.str(), "count": r.uvar(), "time_ms": r.uvar()}
	})
	out["pools"] = listOf(r, func(r *reader) any {
		return map[string]any{"name": r.str(), "used": r.uvar(), "committed": r.uvar(), "max": r.svar()}
	})
	return out
}

func world(r *reader) map[string]any {
	name, flags := r.str(), r.u8()
	out := map[string]any{
		"name": name, "alive": flags&1 != 0, "ticking": flags&2 != 0, "paused": flags&4 != 0,
		"target_tps": r.uvar(), "tick": r.uvar(),
	}
	out["mspt"] = map[string]any{"avg_10s": r.f32(), "avg_1m": r.f32(), "avg_5m": r.f32(), "min_10s": r.f32(), "max_10s": r.f32()}
	out["tps"] = map[string]any{"10s": r.f32(), "1m": r.f32(), "5m": r.f32()}
	out["players"], out["entities"], out["loaded_chunks"] = r.uvar(), r.uvar(), r.uvar()
	out["moon_phase"], out["hour"] = r.svar(), r.svar()
	out["day_progress"], out["game_time_epoch_s"] = r.f32(), r.svar()
	return out
}

func player(r *reader) map[string]any {
	out := map[string]any{"uuid": r.uuid(), "name": r.str(), "world": r.str(), "language": r.str(), "game_mode": r.str()}
	out["ping_ms"] = map[string]any{"last": r.f32(), "avg": r.f32(), "min": r.f32(), "max": r.f32()}
	out["network"] = map[string]any{
		"bytes_sent": r.uvar(), "bytes_received": r.uvar(), "packets_sent": r.uvar(),
		"packets_received": r.uvar(), "queued_packets": r.uvar(),
	}
	out["position"] = map[string]any{"x": r.f64(), "y": r.f64(), "z": r.f64(), "yaw": r.f32(), "pitch": r.f32()}
	out["vitals"] = map[string]any{"health": vital(r), "stamina": vital(r), "mana": vital(r), "oxygen": vital(r)}
	return out
}

func plugins(r *reader) map[string]any {
	return map[string]any{"plugins": listOf(r, func(r *reader) any {
		return map[string]any{"id": r.str(), "version": r.str(), "state": r.str()}
	})}
}

func rawDump(r *reader) map[string]any {
	doc, err := decodeBSON(r.take(r.remaining()))
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"dump": doc}
}

// ---- events (the age field is consumed by the caller) ------------------------

func permission(r *reader) map[string]any {
	op, ok := permissionOperations[r.u8()]
	if !ok {
		op = "UNKNOWN"
	}
	return map[string]any{
		"operation": op, "subject": r.uuid(), "group": r.str(),
		"values": listOf(r, func(r *reader) any { return r.str() }),
	}
}

func noFields(*reader) map[string]any { return nil }

var snapshots = map[uint8]recordSpec{
	0x01: {"HELLO", hello}, 0x02: {"SERVER", server}, 0x03: {"JVM", jvm}, 0x04: {"WORLD", world},
	0x05: {"PLAYER", player}, 0x06: {"RAW_DUMP", rawDump}, 0x07: {"PLUGINS", plugins},
}

var events = map[uint8]recordSpec{
	0x20: {"SERVER_BOOT", noFields},
	0x21: {"SERVER_SHUTDOWN", func(r *reader) map[string]any { return map[string]any{"reason": r.str()} }},
	0x22: {"WORLD_ADDED", func(r *reader) map[string]any { return map[string]any{"world": r.str()} }},
	0x23: {"WORLD_REMOVED", func(r *reader) map[string]any { return map[string]any{"world": r.str(), "reason": r.str()} }},
	0x24: {"WORLD_STARTED", func(r *reader) map[string]any { return map[string]any{"world": r.str()} }},
	0x25: {"ALL_WORLDS_LOADED", noFields},
	0x26: {"PLUGIN_SETUP", func(r *reader) map[string]any { return map[string]any{"plugin": r.str()} }},
	0x30: {"PLAYER_AUTH", func(r *reader) map[string]any {
		m := playerIdent(r)
		m["referral"], m["refused"] = r.bool(), r.bool()
		return m
	}},
	0x31: {"PLAYER_JOIN", withWorld},
	0x32: {"PLAYER_QUIT", func(r *reader) map[string]any {
		m := playerIdent(r)
		m["server_reason"], m["client_reason"], m["session_ms"] = r.str(), r.str(), r.uvar()
		return m
	}},
	0x33: {"PLAYER_SETUP_QUIT", func(r *reader) map[string]any {
		m := playerIdent(r)
		m["server_reason"], m["client_reason"] = r.str(), r.str()
		return m
	}},
	0x34: {"PLAYER_READY", withWorld},
	0x35: {"PLAYER_WORLD_ENTER", withWorld},
	0x36: {"PLAYER_WORLD_LEAVE", withWorld},
	0x37: {"PLAYER_CHAT", func(r *reader) map[string]any {
		m := playerIdent(r)
		m["content"], m["cancelled"], m["recipients"] = r.str(), r.bool(), r.uvar()
		return m
	}},
	0x38: {"PERMISSION_CHANGE", permission},
	0x40: {"BLOCK_BREAK", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "pos": pos(r), "block": r.str(), "tool": r.str()}
	}},
	0x41: {"BLOCK_PLACE", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "pos": pos(r), "item": r.str()}
	}},
	0x42: {"BLOCK_DAMAGE", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "pos": pos(r), "block": r.str(), "damage": r.f32(), "current_damage": r.f32()}
	}},
	0x43: {"BLOCK_USE", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "pos": pos(r), "block": r.str(), "interaction": r.str()}
	}},
	0x44: {"ITEM_DROP", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "item": r.str(), "quantity": r.uvar()}
	}},
	0x45: {"ITEM_PICKUP", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "item": r.str(), "quantity": r.uvar()}
	}},
	0x46: {"CRAFT", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "recipe": r.str(), "quantity": r.uvar()}
	}},
	0x47: {"GAMEMODE_CHANGE", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "game_mode": r.str()}
	}},
	0x48: {"ZONE_DISCOVER", func(r *reader) map[string]any {
		return map[string]any{"actor": actor(r), "world": r.str(), "zone": r.str(), "region": r.str()}
	}},
	0x49: {"RESPAWN", func(r *reader) map[string]any { return map[string]any{"actor": actor(r), "world": r.str()} }},
	0x4A: {"DEATH", func(r *reader) map[string]any {
		return map[string]any{"victim": actor(r), "world": r.str(), "cause": r.str(), "killer": actor(r), "last_damage": r.f32()}
	}},
	0x4B: {"DAMAGE", func(r *reader) map[string]any {
		return map[string]any{"victim": actor(r), "world": r.str(), "cause": r.str(), "attacker": actor(r), "amount": r.f32()}
	}},
}

// DecodeRecords splits a message payload into records. A record of an unknown
// type is returned as KindUnknown (its length prefix lets it be skipped); a
// record that cannot be decoded is dropped and the first such error is
// returned along with every record read successfully.
func DecodeRecords(m *Message) ([]Record, error) {
	r := newReader(m.Payload)
	var out []Record
	var firstErr error
	for r.remaining() > 0 {
		typ := r.u8()
		length := r.uvar()
		if r.err() != nil || length > uint64(r.remaining()) {
			return out, ErrTruncated
		}
		body := newReader(r.take(int(length)))
		rec, err := decodeRecord(typ, body, m.Header.TimeMs)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("record 0x%02X: %w", typ, err)
			}
			continue
		}
		out = append(out, rec)
	}
	return out, firstErr
}

func decodeRecord(typ uint8, body *reader, datagramMs int64) (Record, error) {
	if spec, ok := snapshots[typ]; ok {
		rec := Record{Type: spec.name, Kind: KindSnapshot, TimeMs: datagramMs, Data: spec.decode(body)}
		return rec, body.err()
	}
	if spec, ok := events[typ]; ok {
		age := body.uvar()
		rec := Record{Type: spec.name, Kind: KindEvent, TimeMs: datagramMs - int64(age), Data: spec.decode(body)}
		return rec, body.err()
	}
	return Record{Type: fmt.Sprintf("UNKNOWN_0x%02X", typ), Kind: KindUnknown, TimeMs: datagramMs}, nil
}
