package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	radarBucketKeyPrefix         = "radar:quota:bucket:"
	radarBucketIndexKey          = "radar:quota:buckets"
	radarAASourceKey             = "radar:degradation:aa"
	radarAAPerfKeyPrefix         = "radar:degradation:aa:perf:"
	radarLMArenaSourceKey        = "radar:degradation:lmarena"
	radarClaudeHealthKey         = "radar:health:claude"
	radarOpenAIHealthKey         = "radar:health:openai"
	radarWindsurfHealthKey       = "radar:health:windsurf"
	radarDeepSeekHealthKey       = "radar:health:deepseek"
	radarKimiHealthKey           = "radar:health:kimi"
	radarMiniMaxGlobalHealthKey  = "radar:health:minimax:global"
	radarMiniMaxChinaHealthKey   = "radar:health:minimax:china"
	radarSourceMetaKey           = "radar:meta:sources"
	radarSourceVersionKey        = "radar:meta:source_versions"
	radarSourceCadenceKey        = "radar:meta:source_cadence"
	radarSourceCadenceVersionKey = "radar:meta:source_cadence_versions"
	radarLockKeyPrefix           = "radar:lock:"
	radarMetricsMemoryKey        = "radar:metrics:memory"
	radarMetricsStateKey         = "radar:metrics:aggregator"
	radarMetricsMaxKeys          = 1000
	radarMetricsMaxLedger        = 2048
	radarMaxExactCadenceVersion  = int64(9007199254740991)
	// One fast-path point plus at most 127 fallback points bounds both Redis
	// transport and decode work when a rolling upgrade leaves legacy entries.
	radarLatestFallbackScanLimit = 128
)

var (
	errInvalidStoredRadarBucketSnapshot = errors.New("stored radar bucket snapshot is invalid")
	errInvalidStoredRadarSourceMeta     = errors.New("stored radar source metadata is invalid")

	radarAppendBucketSnapshotScript = redis.NewScript(`
local incoming_score = tonumber(ARGV[1])
local retention_ms = tonumber(ARGV[3])
local ttl_ms = tonumber(ARGV[4])
if incoming_score == nil or retention_ms == nil or ttl_ms == nil then
  return {-1, 0}
end

local newest = redis.call('ZREVRANGE', KEYS[1], 0, 0, 'WITHSCORES')
local current_max = nil
if #newest == 2 then
  current_max = tonumber(newest[2])
  if current_max == nil then
    return {-1, 0}
  end
elseif #newest ~= 0 then
  return {-1, 0}
end

local anchor = incoming_score
if current_max ~= nil and current_max > anchor then
  anchor = current_max
end
local cutoff = anchor - retention_ms
if incoming_score < cutoff then
  return {0, 0}
end

redis.call('ZREMRANGEBYSCORE', KEYS[1], ARGV[1], ARGV[1])
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', '(' .. tostring(cutoff))
redis.call('SADD', KEYS[2], ARGV[5])
if current_max == nil or incoming_score >= current_max then
  redis.call('PEXPIRE', KEYS[1], ttl_ms)
end
local memory_usage = redis.pcall('MEMORY', 'USAGE', KEYS[1])
if type(memory_usage) ~= 'number' then
  return {1, 1}
end
local ledger_write = redis.pcall('HSET', KEYS[3], ARGV[6], memory_usage)
if type(ledger_write) ~= 'number' then
  return {1, 2}
end
return {1, 0}
`)

	radarCleanupBucketIndexScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('HDEL', KEYS[3], ARGV[2])
  return redis.call('SREM', KEYS[2], ARGV[1])
end
return 0
`)

	radarReleaseLockScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

	radarCommitAggregatorMetricsScript = redis.NewScript(`
local candidate_version = tonumber(ARGV[1])
local bucket_count = tonumber(ARGV[4])
if candidate_version == nil or candidate_version <= 0 or bucket_count == nil or bucket_count < 0 or (ARGV[3] ~= '0' and ARGV[3] ~= '1') or ARGV[5] == '' then
  return -1
end
local current_raw = redis.call('HGET', KEYS[1], 'run_version')
if current_raw then
  local current_version = tonumber(current_raw)
  if current_version == nil then
    return -1
  end
  if candidate_version <= current_version then
    return 0
  end
end
redis.call('HSET', KEYS[1], 'run_version', ARGV[1], 'last_run_at', ARGV[2], 'next_fire_at', ARGV[5])
if ARGV[3] == '1' then
  redis.call('HSET', KEYS[1], 'last_success_at', ARGV[2], 'published_bucket_count', ARGV[4])
end
return 1
`)

	radarCommitSourceSuccessScript = redis.NewScript(`
local function valid_cadence_version(value)
  if type(value) ~= 'string' or value == '' or string.match(value, '^%d+$') == nil then
    return nil
  end
  if string.len(value) > 1 and string.sub(value, 1, 1) == '0' then
    return nil
  end
  local parsed = tonumber(value)
  if parsed == nil or parsed <= 0 or parsed > 9007199254740991 then
    return nil
  end
  return parsed
end

local function valid_cadence_deadline(value)
  if type(value) ~= 'string' then
    return false
  end
  local length = string.len(value)
  if length < 20 or length > 30 or string.sub(value, length, length) ~= 'Z' then
    return false
  end
  if string.match(string.sub(value, 1, 19), '^%d%d%d%d%-%d%d%-%d%dT%d%d:%d%d:%d%d$') == nil then
    return false
  end
  if length == 20 then
    return true
  end
  return string.sub(value, 20, 20) == '.' and string.match(string.sub(value, 21, length - 1), '^%d+$') ~= nil
end

local candidate_version = tonumber(ARGV[5])
if candidate_version == nil then
  return -1
end

local candidate_ok, candidate_meta = pcall(cjson.decode, ARGV[4])
if not candidate_ok or type(candidate_meta) ~= 'table' then
  return -1
end
local candidate_deadline = candidate_meta['next_fire_at']
local candidate_cadence_raw = candidate_meta['cadence_version']
local candidate_has_deadline = candidate_deadline ~= nil and candidate_deadline ~= cjson.null
local candidate_has_version = candidate_cadence_raw ~= nil and candidate_cadence_raw ~= cjson.null
if candidate_has_version and not candidate_has_deadline then
  return -1
end
local candidate_cadence = nil
if candidate_has_deadline and not valid_cadence_deadline(candidate_deadline) then
  return -1
end
if candidate_has_version then
  candidate_cadence = valid_cadence_version(candidate_cadence_raw)
  if candidate_cadence == nil then
    return -1
  end
end

local persisted_deadline = redis.call('HGET', KEYS[4], ARGV[1])
local persisted_cadence_raw = redis.call('HGET', KEYS[5], ARGV[1])
if (persisted_deadline and not persisted_cadence_raw) or (persisted_cadence_raw and not persisted_deadline) then
  return -1
end
local persisted_cadence = nil
if persisted_cadence_raw then
  persisted_cadence = valid_cadence_version(persisted_cadence_raw)
  if persisted_cadence == nil or not valid_cadence_deadline(persisted_deadline) then
    return -1
  end
end

local repair_cadence = false
if candidate_has_deadline and not candidate_has_version then
  local allocated = 1
  if persisted_cadence ~= nil then
    if persisted_cadence >= 9007199254740991 then
      return -1
    end
    allocated = persisted_cadence + 1
  end
  candidate_cadence_raw = string.format('%.0f', allocated)
  candidate_cadence = allocated
  candidate_meta['cadence_version'] = candidate_cadence_raw
  repair_cadence = true
elseif persisted_cadence ~= nil and candidate_cadence ~= nil then
  if persisted_cadence > candidate_cadence then
    candidate_meta['next_fire_at'] = persisted_deadline
    candidate_meta['cadence_version'] = persisted_cadence_raw
  elseif candidate_cadence > persisted_cadence then
    repair_cadence = true
  elseif candidate_deadline ~= persisted_deadline then
    return -1
  end
elseif persisted_cadence ~= nil then
  candidate_meta['next_fire_at'] = persisted_deadline
  candidate_meta['cadence_version'] = persisted_cadence_raw
elseif candidate_cadence ~= nil then
  repair_cadence = true
end

local current_raw = redis.call('HGET', KEYS[3], ARGV[1])
if current_raw then
  local current_version = tonumber(current_raw)
  if current_version == nil then
    return -1
  end
  if candidate_version < current_version then
    return 0
  end
end

redis.call('SET', KEYS[1], ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
redis.call('HSET', KEYS[2], ARGV[1], cjson.encode(candidate_meta))
redis.call('HSET', KEYS[3], ARGV[1], ARGV[5])
if repair_cadence then
  redis.call('HSET', KEYS[4], ARGV[1], candidate_deadline)
  redis.call('HSET', KEYS[5], ARGV[1], candidate_cadence_raw)
end
return 1
`)

	radarCommitSourceFailureScript = redis.NewScript(`
local function is_digits(value)
  return type(value) == 'string' and value ~= '' and string.match(value, '^%d+$') ~= nil
end

local function is_rfc3339_nano(value)
  if type(value) ~= 'string' then
    return false
  end
  local length = string.len(value)
  if length < 20 or length > 35 then
    return false
  end
  if string.sub(value, 5, 5) ~= '-' or string.sub(value, 8, 8) ~= '-' or string.sub(value, 11, 11) ~= 'T' or string.sub(value, 14, 14) ~= ':' or string.sub(value, 17, 17) ~= ':' then
    return false
  end

  local year_raw = string.sub(value, 1, 4)
  local month_raw = string.sub(value, 6, 7)
  local day_raw = string.sub(value, 9, 10)
  local hour_raw = string.sub(value, 12, 13)
  local minute_raw = string.sub(value, 15, 16)
  local second_raw = string.sub(value, 18, 19)
  if not is_digits(year_raw) or not is_digits(month_raw) or not is_digits(day_raw) or not is_digits(hour_raw) or not is_digits(minute_raw) or not is_digits(second_raw) then
    return false
  end

  local year = tonumber(year_raw)
  local month = tonumber(month_raw)
  local day = tonumber(day_raw)
  local hour = tonumber(hour_raw)
  local minute = tonumber(minute_raw)
  local second = tonumber(second_raw)
  if month < 1 or month > 12 or hour < 0 or hour > 23 or minute < 0 or minute > 59 or second < 0 or second > 59 then
    return false
  end

  local days_in_month = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
  local leap_year = (year % 400 == 0) or (year % 4 == 0 and year % 100 ~= 0)
  if leap_year then
    days_in_month[2] = 29
  end
  if day < 1 or day > days_in_month[month] then
    return false
  end

  local position = 20
  if string.sub(value, position, position) == '.' then
    local fraction_start = position + 1
    position = fraction_start
    while position <= length do
      local byte = string.byte(value, position)
      if byte == nil or byte < 48 or byte > 57 then
        break
      end
      position = position + 1
    end
    local fraction_length = position - fraction_start
    if fraction_length < 1 or fraction_length > 9 then
      return false
    end
  end

  local zone_length = length - position + 1
  if zone_length == 1 then
    return string.sub(value, position, position) == 'Z'
  end
  if zone_length ~= 6 then
    return false
  end
  local sign = string.sub(value, position, position)
  if sign ~= '+' and sign ~= '-' then
    return false
  end
  if string.sub(value, position + 3, position + 3) ~= ':' then
    return false
  end
  local offset_hour_raw = string.sub(value, position + 1, position + 2)
  local offset_minute_raw = string.sub(value, position + 4, position + 5)
  if not is_digits(offset_hour_raw) or not is_digits(offset_minute_raw) then
    return false
  end
  local offset_hour = tonumber(offset_hour_raw)
  local offset_minute = tonumber(offset_minute_raw)
  return offset_hour >= 0 and offset_hour <= 23 and offset_minute >= 0 and offset_minute <= 59
end

local function valid_cadence_version(value)
  if type(value) ~= 'string' or value == '' or string.match(value, '^%d+$') == nil then
    return nil
  end
  if string.len(value) > 1 and string.sub(value, 1, 1) == '0' then
    return nil
  end
  local parsed = tonumber(value)
  if parsed == nil or parsed <= 0 or parsed > 9007199254740991 then
    return nil
  end
  return parsed
end

local candidate_version = tonumber(ARGV[3])
if candidate_version == nil then
  return -1
end

local current_meta_raw = redis.call('HGET', KEYS[1], ARGV[1])
local current_version_raw = redis.call('HGET', KEYS[2], ARGV[1])
if (current_meta_raw and not current_version_raw) or (current_version_raw and not current_meta_raw) then
  return -1
end

local candidate_ok, candidate_meta = pcall(cjson.decode, ARGV[2])
if not candidate_ok or type(candidate_meta) ~= 'table' or not is_rfc3339_nano(candidate_meta['last_attempt_at']) or type(candidate_meta['error']) ~= 'string' then
  return -1
end

local candidate_deadline = candidate_meta['next_fire_at']
local candidate_cadence_raw = candidate_meta['cadence_version']
local candidate_has_deadline = candidate_deadline ~= nil and candidate_deadline ~= cjson.null
local candidate_has_version = candidate_cadence_raw ~= nil and candidate_cadence_raw ~= cjson.null
if candidate_has_version and not candidate_has_deadline then
  return -1
end
local candidate_cadence = nil
if candidate_has_deadline and not is_rfc3339_nano(candidate_deadline) then
  return -1
end
if candidate_has_version then
  candidate_cadence = valid_cadence_version(candidate_cadence_raw)
  if candidate_cadence == nil then
    return -1
  end
end

local persisted_deadline = redis.call('HGET', KEYS[3], ARGV[1])
local persisted_cadence_raw = redis.call('HGET', KEYS[4], ARGV[1])
if (persisted_deadline and not persisted_cadence_raw) or (persisted_cadence_raw and not persisted_deadline) then
  return -1
end
local persisted_cadence = nil
if persisted_cadence_raw then
  persisted_cadence = valid_cadence_version(persisted_cadence_raw)
  if persisted_cadence == nil or not is_rfc3339_nano(persisted_deadline) then
    return -1
  end
end

local repair_cadence = false
if candidate_has_deadline and not candidate_has_version then
  local allocated = 1
  if persisted_cadence ~= nil then
    if persisted_cadence >= 9007199254740991 then
      return -1
    end
    allocated = persisted_cadence + 1
  end
  candidate_cadence_raw = string.format('%.0f', allocated)
  candidate_cadence = allocated
  candidate_meta['cadence_version'] = candidate_cadence_raw
  repair_cadence = true
elseif persisted_cadence ~= nil and candidate_cadence ~= nil then
  if persisted_cadence > candidate_cadence then
    candidate_meta['next_fire_at'] = persisted_deadline
    candidate_meta['cadence_version'] = persisted_cadence_raw
  elseif candidate_cadence > persisted_cadence then
    repair_cadence = true
  elseif candidate_deadline ~= persisted_deadline then
    return -1
  end
elseif persisted_cadence ~= nil then
  candidate_meta['next_fire_at'] = persisted_deadline
  candidate_meta['cadence_version'] = persisted_cadence_raw
elseif candidate_cadence ~= nil then
  repair_cadence = true
end

local current_is_success = false
if current_meta_raw then
  local current_ok, current_meta = pcall(cjson.decode, current_meta_raw)
  if not current_ok or type(current_meta) ~= 'table' or not is_rfc3339_nano(current_meta['last_attempt_at']) then
    return -1
  end
  local last_success = current_meta['last_success_at']
  if last_success ~= nil and last_success ~= cjson.null and not is_rfc3339_nano(last_success) then
    return -1
  end
  local current_error = current_meta['error']
  if current_error ~= nil and current_error ~= cjson.null and type(current_error) ~= 'string' then
    return -1
  end
  current_is_success = (last_success ~= nil and last_success ~= cjson.null and (current_error == nil or current_error == cjson.null))
  if last_success == nil then
    candidate_meta['last_success_at'] = cjson.null
  else
    candidate_meta['last_success_at'] = last_success
  end
else
  candidate_meta['last_success_at'] = cjson.null
end

if current_version_raw then
  local current_version = tonumber(current_version_raw)
  if current_version == nil then
    return -1
  end
  if candidate_version < current_version then
    return 0
  end
  -- Equal Unix-millisecond completions prefer a validated success already in
  -- storage. The success script still permits a success to replace a failure.
  if candidate_version == current_version and current_is_success then
    return 0
  end
end

redis.call('HSET', KEYS[1], ARGV[1], cjson.encode(candidate_meta))
redis.call('HSET', KEYS[2], ARGV[1], ARGV[3])
if repair_cadence then
  redis.call('HSET', KEYS[3], ARGV[1], candidate_deadline)
  redis.call('HSET', KEYS[4], ARGV[1], candidate_cadence_raw)
end
return 1
`)

	radarSetSourceMetaScript = redis.NewScript(`
local function valid_cadence_version(value)
  if type(value) ~= 'string' or value == '' or string.match(value, '^%d+$') == nil then
    return nil
  end
  if string.len(value) > 1 and string.sub(value, 1, 1) == '0' then
    return nil
  end
  local parsed = tonumber(value)
  if parsed == nil or parsed <= 0 or parsed > 9007199254740991 then
    return nil
  end
  return parsed
end

local function valid_cadence_deadline(value)
  if type(value) ~= 'string' then
    return false
  end
  local length = string.len(value)
  if length < 20 or length > 30 or string.sub(value, length, length) ~= 'Z' then
    return false
  end
  if string.match(string.sub(value, 1, 19), '^%d%d%d%d%-%d%d%-%d%dT%d%d:%d%d:%d%d$') == nil then
    return false
  end
  if length == 20 then
    return true
  end
  return string.sub(value, 20, 20) == '.' and string.match(string.sub(value, 21, length - 1), '^%d+$') ~= nil
end

local candidate_version = tonumber(ARGV[3])
if candidate_version == nil then
  return -1
end

local candidate_ok, candidate_meta = pcall(cjson.decode, ARGV[2])
if not candidate_ok or type(candidate_meta) ~= 'table' then
  return -1
end
local candidate_deadline = candidate_meta['next_fire_at']
local candidate_cadence_raw = candidate_meta['cadence_version']
local candidate_has_deadline = candidate_deadline ~= nil and candidate_deadline ~= cjson.null
local candidate_has_version = candidate_cadence_raw ~= nil and candidate_cadence_raw ~= cjson.null
if candidate_has_version and not candidate_has_deadline then
  return -1
end
local candidate_cadence = nil
if candidate_has_deadline and not valid_cadence_deadline(candidate_deadline) then
  return -1
end
if candidate_has_version then
  candidate_cadence = valid_cadence_version(candidate_cadence_raw)
  if candidate_cadence == nil then
    return -1
  end
end

local persisted_deadline = redis.call('HGET', KEYS[3], ARGV[1])
local persisted_cadence_raw = redis.call('HGET', KEYS[4], ARGV[1])
if (persisted_deadline and not persisted_cadence_raw) or (persisted_cadence_raw and not persisted_deadline) then
  return -1
end
local persisted_cadence = nil
if persisted_cadence_raw then
  persisted_cadence = valid_cadence_version(persisted_cadence_raw)
  if persisted_cadence == nil or not valid_cadence_deadline(persisted_deadline) then
    return -1
  end
end

local repair_cadence = false
if candidate_has_deadline and not candidate_has_version then
  local allocated = 1
  if persisted_cadence ~= nil then
    if persisted_cadence >= 9007199254740991 then
      return -1
    end
    allocated = persisted_cadence + 1
  end
  candidate_cadence_raw = string.format('%.0f', allocated)
  candidate_cadence = allocated
  candidate_meta['cadence_version'] = candidate_cadence_raw
  repair_cadence = true
elseif persisted_cadence ~= nil and candidate_cadence ~= nil then
  if persisted_cadence > candidate_cadence then
    candidate_meta['next_fire_at'] = persisted_deadline
    candidate_meta['cadence_version'] = persisted_cadence_raw
  elseif candidate_cadence > persisted_cadence then
    repair_cadence = true
  elseif candidate_deadline ~= persisted_deadline then
    return -1
  end
elseif persisted_cadence ~= nil then
  candidate_meta['next_fire_at'] = persisted_deadline
  candidate_meta['cadence_version'] = persisted_cadence_raw
elseif candidate_cadence ~= nil then
  repair_cadence = true
end

local current_raw = redis.call('HGET', KEYS[2], ARGV[1])
if current_raw then
  local current_version = tonumber(current_raw)
  if current_version == nil then
    return -1
  end
  if candidate_version < current_version then
    return 0
  end
end

redis.call('HSET', KEYS[1], ARGV[1], cjson.encode(candidate_meta))
redis.call('HSET', KEYS[2], ARGV[1], ARGV[3])
if repair_cadence then
  redis.call('HSET', KEYS[3], ARGV[1], candidate_deadline)
  redis.call('HSET', KEYS[4], ARGV[1], candidate_cadence_raw)
end
return 1
`)

	radarAdvanceSourceNextFireScript = redis.NewScript(`
local function valid_cadence_version(value)
  if type(value) ~= 'string' or value == '' or string.match(value, '^%d+$') == nil then
    return nil
  end
  if string.len(value) > 1 and string.sub(value, 1, 1) == '0' then
    return nil
  end
  local parsed = tonumber(value)
  if parsed == nil or parsed <= 0 or parsed > 9007199254740991 then
    return nil
  end
  return parsed
end

local function valid_cadence_deadline(value)
  if type(value) ~= 'string' then
    return false
  end
  local length = string.len(value)
  if length < 20 or length > 30 or string.sub(value, length, length) ~= 'Z' then
    return false
  end
  if string.match(string.sub(value, 1, 19), '^%d%d%d%d%-%d%d%-%d%dT%d%d:%d%d:%d%d$') == nil then
    return false
  end
  if length == 20 then
    return true
  end
  return string.sub(value, 20, 20) == '.' and string.match(string.sub(value, 21, length - 1), '^%d+$') ~= nil
end

if not valid_cadence_deadline(ARGV[2]) then
  return -1
end
local current_deadline = redis.call('HGET', KEYS[1], ARGV[1])
local current_version_raw = redis.call('HGET', KEYS[2], ARGV[1])
if (current_deadline and not current_version_raw) or (current_version_raw and not current_deadline) then
  return -1
end
local next_version = 1
if current_version_raw then
  local current_version = valid_cadence_version(current_version_raw)
  if current_version == nil or not valid_cadence_deadline(current_deadline) then
    return -1
  end
  if current_version >= 9007199254740991 then
    return -1
  end
  next_version = current_version + 1
end
local next_version_raw = string.format('%.0f', next_version)

local encoded_meta = nil
local current_meta_raw = redis.call('HGET', KEYS[3], ARGV[1])
if current_meta_raw then
  local current_ok, current_meta = pcall(cjson.decode, current_meta_raw)
  if not current_ok or type(current_meta) ~= 'table' then
    return -1
  end
  current_meta['next_fire_at'] = ARGV[2]
  current_meta['cadence_version'] = next_version_raw
  encoded_meta = cjson.encode(current_meta)
end

redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
redis.call('HSET', KEYS[2], ARGV[1], next_version_raw)
if encoded_meta then
  redis.call('HSET', KEYS[3], ARGV[1], encoded_meta)
end
return {ARGV[2], next_version_raw}
`)

	radarGetSourceCadenceScript = redis.NewScript(`
local deadline = redis.call('HGET', KEYS[1], ARGV[1])
local version = redis.call('HGET', KEYS[2], ARGV[1])
if not deadline and not version then
  return {}
end
if not deadline or not version then
  return {-1}
end
return {deadline, version}
`)
)

type radarCacheRepository struct {
	rdb                       *redis.Client
	quotaHistoryRetention     time.Duration
	quotaAggregationInterval  time.Duration
	externalResponseMaxBytes  int64
	sourceHardRetentionWindow time.Duration
	metrics                   *observability.RadarMetrics
	metricsSources            []service.RadarSourceKey
}

var _ service.RadarCacheRepository = (*radarCacheRepository)(nil)
var _ service.RadarSourceCadenceRepository = (*radarCacheRepository)(nil)

// NewRadarCacheRepository creates the Redis-backed Model Radar cache and fails
// eagerly when dependencies or the configuration fields it uses are invalid.
func NewRadarCacheRepository(rdb *redis.Client, cfg *config.Config) (service.RadarCacheRepository, error) {
	if rdb == nil {
		return nil, errors.New("radar cache repository requires Redis")
	}
	if cfg == nil {
		return nil, errors.New("radar cache repository requires config")
	}
	if cfg.Radar.QuotaHistoryRetentionDays <= 0 {
		return nil, errors.New("radar quota history retention must be positive")
	}
	if cfg.Radar.QuotaAggregatorIntervalMin <= 0 {
		return nil, errors.New("radar quota aggregation interval must be positive")
	}
	if cfg.Radar.ExternalResponseMaxBytes <= 0 {
		return nil, errors.New("radar external response maximum must be positive")
	}
	if cfg.Radar.SourceHardRetentionDays <= 0 {
		return nil, errors.New("radar source hard retention must be positive")
	}

	metricsSources := []service.RadarSourceKey{
		service.RadarSourceAA,
		service.RadarSourceLMArena,
		service.RadarSourceStatusClaude,
		service.RadarSourceStatusOpenAI,
		service.RadarSourceStatusWindsurf,
		service.RadarSourceStatusDeepSeek,
		service.RadarSourceStatusKimi,
		service.RadarSourceStatusMiniMaxGlobal,
		service.RadarSourceStatusMiniMaxChina,
	}
	for _, model := range cfg.Radar.ArtificialAnalysisModelSlugs {
		source, err := service.RadarAAPerformanceSource(model)
		if err != nil {
			return nil, errors.New("radar cache repository requires valid AA model slug")
		}
		metricsSources = append(metricsSources, source)
	}

	return &radarCacheRepository{
		rdb:                       rdb,
		quotaHistoryRetention:     time.Duration(cfg.Radar.QuotaHistoryRetentionDays) * 24 * time.Hour,
		quotaAggregationInterval:  time.Duration(cfg.Radar.QuotaAggregatorIntervalMin) * time.Minute,
		externalResponseMaxBytes:  cfg.Radar.ExternalResponseMaxBytes,
		sourceHardRetentionWindow: time.Duration(cfg.Radar.SourceHardRetentionDays) * 24 * time.Hour,
		metrics:                   observability.DefaultRadarMetrics(),
		metricsSources:            metricsSources,
	}, nil
}

// AppendBucketSnapshot atomically anchors retention at the newest observed
// capture. Accepted older points do not refresh TTL; an equal-score correction
// replaces the newest snapshot and refreshes its full retention.
func (r *radarCacheRepository) AppendBucketSnapshot(ctx context.Context, snapshot service.BucketSnapshotDTO) error {
	if err := validateRadarSnapshot(snapshot); err != nil {
		return err
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode radar bucket snapshot: %w", err)
	}

	key := radarBucketRedisKey(snapshot.BucketKey)
	scoreMillis := snapshot.CapturedAt.UnixMilli()
	ttl := r.quotaHistoryRetention + 2*r.quotaAggregationInterval
	result, err := radarAppendBucketSnapshotScript.Run(
		ctx,
		r.rdb,
		[]string{key, radarBucketIndexKey, radarMetricsMemoryKey},
		scoreMillis,
		encoded,
		durationMillisecondsCeil(r.quotaHistoryRetention),
		durationMillisecondsCeil(ttl),
		snapshot.BucketKey,
		radarMetricsLedgerField("quota_bucket", key),
	).Slice()
	if err != nil || len(result) != 2 {
		r.metrics.RecordRedis("append_bucket", "write", errors.New("operation failed"))
		return errors.New("append radar bucket snapshot failed")
	}
	dataResult, dataOK := result[0].(int64)
	ledgerResult, ledgerOK := result[1].(int64)
	if !dataOK || !ledgerOK || dataResult < 0 || dataResult > 1 || ledgerResult < 0 || ledgerResult > 2 {
		r.metrics.RecordRedis("append_bucket", "write", errors.New("operation failed"))
		return errors.New("append radar bucket snapshot failed")
	}
	r.metrics.RecordRedis("append_bucket", "write", nil)
	if dataResult == 1 {
		switch ledgerResult {
		case 0:
			r.metrics.RecordRedis("cache_size", "read", nil)
			r.metrics.RecordRedis("cache_size", "write", nil)
		case 1:
			r.metrics.RecordRedis("cache_size", "read", errors.New("operation failed"))
		case 2:
			r.metrics.RecordRedis("cache_size", "read", nil)
			r.metrics.RecordRedis("cache_size", "write", errors.New("operation failed"))
		}
	}
	r.updateRadarMemoryLedger(ctx, "metadata", radarBucketIndexKey)
	return nil
}

func (r *radarCacheRepository) ListBucketKeys(ctx context.Context) ([]string, error) {
	indexed, err := r.rdb.SMembers(ctx, radarBucketIndexKey).Result()
	if err != nil {
		r.metrics.RecordRedis("list_buckets", "read", err)
		return nil, fmt.Errorf("list radar bucket index: %w", err)
	}
	if len(indexed) == 0 {
		r.metrics.RecordRedis("list_buckets", "read", nil)
		return make([]string, 0), nil
	}

	for _, bucketKey := range indexed {
		if _, _, err := service.ParseRadarBucketKey(bucketKey); err != nil {
			return nil, fmt.Errorf("validate indexed radar bucket: %w", err)
		}
	}

	pipe := r.rdb.Pipeline()
	existsCommands := make([]*redis.IntCmd, len(indexed))
	for i, bucketKey := range indexed {
		existsCommands[i] = pipe.Exists(ctx, radarBucketRedisKey(bucketKey))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		r.metrics.RecordRedis("list_buckets", "read", err)
		return nil, fmt.Errorf("check indexed radar buckets: %w", err)
	}

	active := make([]string, 0, len(indexed))
	missingCandidates := make([]string, 0)
	for i, bucketKey := range indexed {
		if existsCommands[i].Val() == 0 {
			missingCandidates = append(missingCandidates, bucketKey)
			continue
		}
		active = append(active, bucketKey)
	}
	if err := r.cleanupMissingBucketIndexEntries(ctx, missingCandidates); err != nil {
		r.metrics.RecordRedis("list_buckets", "write", err)
		return nil, err
	}

	sort.Strings(active)
	r.metrics.RecordRedis("list_buckets", "read", nil)
	return active, nil
}

func (r *radarCacheRepository) cleanupMissingBucketIndexEntries(ctx context.Context, candidates []string) error {
	if len(candidates) == 0 {
		return nil
	}
	for _, bucketKey := range candidates {
		if _, _, err := service.ParseRadarBucketKey(bucketKey); err != nil {
			return fmt.Errorf("validate radar bucket cleanup candidate: %w", err)
		}
	}

	pipe := r.rdb.Pipeline()
	for _, bucketKey := range candidates {
		// Queue EVAL directly: Script.Run cannot observe NOSCRIPT until a pipeline
		// executes, while EVAL preserves one round trip for all candidates.
		radarCleanupBucketIndexScript.Eval(
			ctx,
			pipe,
			[]string{radarBucketRedisKey(bucketKey), radarBucketIndexKey, radarMetricsMemoryKey},
			bucketKey,
			radarMetricsLedgerField("quota_bucket", radarBucketRedisKey(bucketKey)),
		)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("clean radar bucket index: %w", err)
	}
	return nil
}

func (r *radarCacheRepository) GetLatestBucket(ctx context.Context, bucketKey string) (*service.BucketSnapshotDTO, error) {
	if _, _, err := service.ParseRadarBucketKey(bucketKey); err != nil {
		return nil, err
	}

	values, err := r.rdb.ZRevRange(ctx, radarBucketRedisKey(bucketKey), 0, 0).Result()
	if err != nil {
		r.metrics.RecordRedis("get_latest_bucket", "read", err)
		return nil, fmt.Errorf("read latest radar bucket snapshot: %w", err)
	}
	if len(values) == 0 {
		r.metrics.RecordRedis("get_latest_bucket", "read", nil)
		return nil, service.ErrRadarCacheMiss
	}

	snapshot, err := decodeRadarSnapshot(values[0], bucketKey)
	if err == nil {
		r.metrics.RecordRedis("get_latest_bucket", "read", nil)
		return &snapshot, nil
	}

	// Preserve the one-command normal path. Only a malformed newest entry pays
	// for one bounded fallback command, never an unbounded history read.
	fallback, readErr := r.rdb.ZRevRange(
		ctx,
		radarBucketRedisKey(bucketKey),
		1,
		radarLatestFallbackScanLimit-1,
	).Result()
	if readErr != nil {
		r.metrics.RecordRedis("get_latest_bucket", "read", readErr)
		return nil, fmt.Errorf("read latest radar bucket fallback: %w", readErr)
	}
	for _, encoded := range fallback {
		candidate, decodeErr := decodeRadarSnapshot(encoded, bucketKey)
		if decodeErr == nil {
			r.metrics.RecordRedis("get_latest_bucket", "read", nil)
			return &candidate, nil
		}
	}
	r.metrics.RecordRedis("get_latest_bucket", "read", nil)
	return nil, service.ErrRadarCacheMiss
}

func (r *radarCacheRepository) GetBucketTrend(ctx context.Context, bucketKey string, since time.Time) ([]service.BucketSnapshotDTO, error) {
	if _, _, err := service.ParseRadarBucketKey(bucketKey); err != nil {
		return nil, err
	}

	values, err := r.rdb.ZRangeByScore(ctx, radarBucketRedisKey(bucketKey), &redis.ZRangeBy{
		Min: strconv.FormatInt(since.UnixMilli(), 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		r.metrics.RecordRedis("get_bucket_trend", "read", err)
		return nil, fmt.Errorf("read radar bucket trend: %w", err)
	}

	snapshots := make([]service.BucketSnapshotDTO, 0, len(values))
	for _, value := range values {
		snapshot, err := decodeRadarSnapshot(value, bucketKey)
		if err != nil {
			continue
		}
		if snapshot.CapturedAt.Before(since) {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	r.metrics.RecordRedis("get_bucket_trend", "read", nil)
	return snapshots, nil
}

// SetSourcePayload is a low-level payload-only operation for repair and tests.
// Production successful fetches must use CommitSourceSuccess so payload and
// metadata advance atomically.
func (r *radarCacheRepository) SetSourcePayload(
	ctx context.Context,
	source service.RadarSourceKey,
	payload []byte,
	retention time.Duration,
) error {
	key, err := r.validateSourcePayloadWrite(source, payload, retention)
	if err != nil {
		return err
	}

	if err := r.rdb.Set(ctx, key, payload, retention).Err(); err != nil {
		r.metrics.RecordRedis("set_source", "write", err)
		return fmt.Errorf("store radar source payload: %w", err)
	}
	r.metrics.RecordRedis("set_source", "write", nil)
	r.updateRadarMemoryLedger(ctx, radarMetricsCacheFamily(source), key)
	return nil
}

func (r *radarCacheRepository) GetSourcePayload(ctx context.Context, source service.RadarSourceKey) ([]byte, error) {
	key, err := radarSourceRedisKey(source)
	if err != nil {
		return nil, err
	}

	payload, err := r.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			r.metrics.RecordRedis("get_source", "read", nil)
			return nil, service.ErrRadarCacheMiss
		}
		r.metrics.RecordRedis("get_source", "read", err)
		return nil, fmt.Errorf("read radar source payload: %w", err)
	}
	r.metrics.RecordRedis("get_source", "read", nil)
	return payload, nil
}

// CommitSourceSuccess atomically publishes payload and metadata when the fetch
// attempt is not older than the last committed attempt for this source.
func (r *radarCacheRepository) CommitSourceSuccess(
	ctx context.Context,
	source service.RadarSourceKey,
	payload []byte,
	retention time.Duration,
	meta service.SourceFetchMeta,
) (bool, error) {
	key, err := r.validateSourcePayloadWrite(source, payload, retention)
	if err != nil {
		return false, err
	}
	if err := validateSourceFetchMeta(meta); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return false, errors.New("encode radar source metadata failed")
	}

	result, err := radarCommitSourceSuccessScript.Run(
		ctx,
		r.rdb,
		[]string{key, radarSourceMetaKey, radarSourceVersionKey, radarSourceCadenceKey, radarSourceCadenceVersionKey},
		string(source),
		payload,
		durationMillisecondsCeil(retention),
		encoded,
		meta.LastAttemptAt.UnixMilli(),
	).Int64()
	if err != nil || result < 0 || result > 1 {
		r.metrics.RecordRedis("commit_success", "write", errors.New("operation failed"))
		return false, errors.New("commit radar source success failed")
	}
	r.metrics.RecordRedis("commit_success", "write", nil)
	if result == 1 {
		r.updateRadarMemoryLedger(ctx, radarMetricsCacheFamily(source), key)
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceMetaKey)
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceVersionKey)
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceKey)
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceVersionKey)
	}
	return result == 1, nil
}

// CommitSourceFailure atomically publishes only failure metadata. The Lua CAS
// preserves the previously stored last_success_at, gives an equal-version
// stored success precedence, and never reads or writes the source payload key.
func (r *radarCacheRepository) CommitSourceFailure(
	ctx context.Context,
	source service.RadarSourceKey,
	meta service.SourceFetchMeta,
) (bool, error) {
	if _, err := radarSourceRedisKey(source); err != nil {
		return false, err
	}
	if err := validateSourceFetchMeta(meta); err != nil {
		return false, err
	}
	if meta.Error == nil {
		return false, errors.New("radar source failure metadata requires an error code")
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return false, errors.New("encode radar source failure metadata failed")
	}

	result, err := radarCommitSourceFailureScript.Run(
		ctx,
		r.rdb,
		[]string{radarSourceMetaKey, radarSourceVersionKey, radarSourceCadenceKey, radarSourceCadenceVersionKey},
		string(source),
		encoded,
		meta.LastAttemptAt.UnixMilli(),
	).Int64()
	if err != nil || result < 0 || result > 1 {
		r.metrics.RecordRedis("commit_failure", "write", errors.New("operation failed"))
		return false, errors.New("commit radar source failure failed")
	}
	r.metrics.RecordRedis("commit_failure", "write", nil)
	if result == 1 {
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceMetaKey)
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceVersionKey)
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceKey)
		r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceVersionKey)
	}
	return result == 1, nil
}

// SetSourceMeta advances metadata monotonically and ignores older attempts. It
// must not be paired with SetSourcePayload to publish a successful fetch;
// production success paths must use CommitSourceSuccess.
func (r *radarCacheRepository) SetSourceMeta(ctx context.Context, source service.RadarSourceKey, meta service.SourceFetchMeta) error {
	if _, err := radarSourceRedisKey(source); err != nil {
		return err
	}
	if err := validateSourceFetchMeta(meta); err != nil {
		return err
	}

	encoded, err := json.Marshal(meta)
	if err != nil {
		return errors.New("encode radar source metadata failed")
	}
	result, err := radarSetSourceMetaScript.Run(
		ctx,
		r.rdb,
		[]string{radarSourceMetaKey, radarSourceVersionKey, radarSourceCadenceKey, radarSourceCadenceVersionKey},
		string(source),
		encoded,
		meta.LastAttemptAt.UnixMilli(),
	).Int64()
	if err != nil || result < 0 || result > 1 {
		r.metrics.RecordRedis("set_meta", "write", errors.New("operation failed"))
		return errors.New("store radar source metadata failed")
	}
	r.metrics.RecordRedis("set_meta", "write", nil)
	r.updateRadarMemoryLedger(ctx, "metadata", radarSourceMetaKey)
	r.updateRadarMemoryLedger(ctx, "metadata", radarSourceVersionKey)
	r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceKey)
	r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceVersionKey)
	return nil
}

// AdvanceSourceNextFire atomically advances the real scheduler cadence and, if
// fetch metadata already exists, patches only its next_fire_at field. Fetch
// commit scripts read this same cadence key so either Redis command ordering
// leaves the newest deadline visible.
func (r *radarCacheRepository) AdvanceSourceNextFire(ctx context.Context, source service.RadarSourceKey, nextFireAt time.Time) (service.RadarSourceCadence, error) {
	if _, err := radarSourceRedisKey(source); err != nil {
		return service.RadarSourceCadence{}, err
	}
	if nextFireAt.IsZero() {
		return service.RadarSourceCadence{}, errors.New("radar source next fire is required")
	}
	nextFireAt = nextFireAt.UTC()
	result, err := radarAdvanceSourceNextFireScript.Run(
		ctx,
		r.rdb,
		[]string{radarSourceCadenceKey, radarSourceCadenceVersionKey, radarSourceMetaKey},
		string(source),
		nextFireAt.Format(time.RFC3339Nano),
	).Slice()
	if err != nil || len(result) != 2 {
		r.metrics.RecordRedis("source_cadence", "write", errors.New("operation failed"))
		return service.RadarSourceCadence{}, errors.New("store radar source cadence failed")
	}
	deadlineRaw, deadlineOK := result[0].(string)
	versionRaw, versionOK := result[1].(string)
	cadence := service.RadarSourceCadence{NextFireAt: nextFireAt, Version: versionRaw}
	if !deadlineOK || !versionOK || deadlineRaw != nextFireAt.Format(time.RFC3339Nano) || validateRadarSourceCadence(cadence) != nil {
		r.metrics.RecordRedis("source_cadence", "write", errors.New("operation failed"))
		return service.RadarSourceCadence{}, errors.New("store radar source cadence failed")
	}
	r.metrics.RecordRedis("source_cadence", "write", nil)
	r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceKey)
	r.updateRadarMemoryLedger(ctx, "metadata", radarSourceCadenceVersionKey)
	r.updateRadarMemoryLedger(ctx, "metadata", radarSourceMetaKey)
	return cadence, nil
}

func (r *radarCacheRepository) GetSourceCadence(ctx context.Context, source service.RadarSourceKey) (service.RadarSourceCadence, error) {
	if _, err := radarSourceRedisKey(source); err != nil {
		return service.RadarSourceCadence{}, err
	}
	result, err := radarGetSourceCadenceScript.Run(
		ctx,
		r.rdb,
		[]string{radarSourceCadenceKey, radarSourceCadenceVersionKey},
		string(source),
	).Slice()
	if err != nil {
		return service.RadarSourceCadence{}, errors.New("read radar source cadence failed")
	}
	if len(result) == 0 {
		return service.RadarSourceCadence{}, service.ErrRadarCacheMiss
	}
	if len(result) != 2 {
		return service.RadarSourceCadence{}, errors.New("read radar source cadence failed")
	}
	deadlineRaw, deadlineOK := result[0].(string)
	versionRaw, versionOK := result[1].(string)
	deadline, parseErr := time.Parse(time.RFC3339Nano, deadlineRaw)
	cadence := service.RadarSourceCadence{NextFireAt: deadline.UTC(), Version: versionRaw}
	if !deadlineOK || !versionOK || parseErr != nil || validateRadarSourceCadence(cadence) != nil {
		return service.RadarSourceCadence{}, errors.New("read radar source cadence failed")
	}
	return cadence, nil
}

func (r *radarCacheRepository) ListSourceMeta(ctx context.Context) (map[service.RadarSourceKey]service.SourceFetchMeta, error) {
	stored, err := r.rdb.HGetAll(ctx, radarSourceMetaKey).Result()
	if err != nil {
		r.metrics.RecordRedis("list_meta", "read", err)
		return nil, fmt.Errorf("list radar source metadata: %w", err)
	}

	result := make(map[service.RadarSourceKey]service.SourceFetchMeta, len(stored))
	for rawSource, encoded := range stored {
		source := service.RadarSourceKey(rawSource)
		if _, err := radarSourceRedisKey(source); err != nil {
			return nil, fmt.Errorf("validate stored radar source metadata key: %w", err)
		}

		var meta service.SourceFetchMeta
		if err := json.Unmarshal([]byte(encoded), &meta); err != nil {
			return nil, errInvalidStoredRadarSourceMeta
		}
		if err := validateSourceFetchMeta(meta); err != nil {
			return nil, errInvalidStoredRadarSourceMeta
		}
		result[source] = meta
	}
	r.metrics.RecordRedis("list_meta", "read", nil)
	return result, nil
}

func (r *radarCacheRepository) TryLock(ctx context.Context, task, owner string, ttl time.Duration) (bool, error) {
	if err := validateRadarLockIdentity(task, owner); err != nil {
		return false, err
	}
	if ttl <= 0 {
		return false, errors.New("radar lock ttl must be positive")
	}

	acquired, err := r.rdb.SetNX(ctx, radarLockKeyPrefix+task, owner, ttl).Result()
	if err != nil {
		r.metrics.RecordRedis("try_lock", "lock", err)
		return false, fmt.Errorf("acquire radar lock: %w", err)
	}
	r.metrics.RecordRedis("try_lock", "lock", nil)
	return acquired, nil
}

func (r *radarCacheRepository) ReleaseLock(ctx context.Context, task, owner string) error {
	if err := validateRadarLockIdentity(task, owner); err != nil {
		return err
	}
	result, err := radarReleaseLockScript.Run(
		ctx,
		r.rdb,
		[]string{radarLockKeyPrefix + task},
		owner,
	).Int64()
	if err != nil || result < 0 || result > 1 {
		r.metrics.RecordRedis("release_lock", "lock", errors.New("operation failed"))
		return errors.New("release radar lock failed")
	}
	r.metrics.RecordRedis("release_lock", "lock", nil)
	return nil
}

func (r *radarCacheRepository) CommitRadarAggregatorRun(ctx context.Context, state service.RadarAggregatorRunState) (bool, error) {
	if state.RunVersion <= 0 || state.CompletedAt.IsZero() || state.NextFireAt.IsZero() || state.PublishedBucketCount < 0 {
		return false, errors.New("invalid radar aggregator metrics state")
	}
	success := "0"
	if state.Success {
		success = "1"
	}
	result, err := radarCommitAggregatorMetricsScript.Run(
		ctx,
		r.rdb,
		[]string{radarMetricsStateKey},
		state.RunVersion,
		state.CompletedAt.UTC().Format(time.RFC3339Nano),
		success,
		state.PublishedBucketCount,
		state.NextFireAt.UTC().Format(time.RFC3339Nano),
	).Int64()
	if err != nil || result < 0 || result > 1 {
		r.metrics.RecordRedis("aggregator_state", "write", errors.New("operation failed"))
		return false, errors.New("store radar aggregator metrics state failed")
	}
	r.metrics.RecordRedis("aggregator_state", "write", nil)
	if result == 1 {
		r.updateRadarMemoryLedger(ctx, "metadata", radarMetricsStateKey)
	}
	return result == 1, nil
}

func radarMetricsCacheFamily(source service.RadarSourceKey) string {
	switch source {
	case service.RadarSourceAA:
		return "aa"
	case service.RadarSourceLMArena:
		return "lmarena"
	case service.RadarSourceStatusClaude, service.RadarSourceStatusOpenAI,
		service.RadarSourceStatusWindsurf, service.RadarSourceStatusDeepSeek,
		service.RadarSourceStatusKimi, service.RadarSourceStatusMiniMaxGlobal,
		service.RadarSourceStatusMiniMaxChina:
		return "service_health"
	default:
		if strings.HasPrefix(string(source), "aa_perf:") {
			return "aa_performance"
		}
		return "other"
	}
}

func radarMetricsPrivateIdentity(key string) string {
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", digest[:])
}

func radarMetricsLedgerField(family, key string) string {
	return family + ":" + radarMetricsPrivateIdentity(key)
}

func parseRadarMetricsLedgerField(field string) (string, bool) {
	family, digest, ok := strings.Cut(field, ":")
	if !ok || normalizeRadarMetricsCacheFamily(family) != family || len(digest) != sha256.Size*2 {
		return "", false
	}
	decoded, err := hex.DecodeString(digest)
	return family, err == nil && len(decoded) == sha256.Size
}

func normalizeRadarMetricsCacheFamily(family string) string {
	switch family {
	case "quota_bucket", "aa", "aa_performance", "lmarena", "service_health", "metadata":
		return family
	default:
		return ""
	}
}

func (r *radarCacheRepository) updateRadarMemoryLedger(ctx context.Context, family, key string) {
	_, _, _ = r.writeRadarMemoryLedgerEntry(ctx, family, key)
}

func (r *radarCacheRepository) writeRadarMemoryLedgerEntry(ctx context.Context, family, key string) (int64, bool, error) {
	field := radarMetricsLedgerField(family, key)
	usage, err := r.rdb.MemoryUsage(ctx, key).Result()
	if err == redis.Nil {
		r.metrics.RecordRedis("cache_size", "read", nil)
		if err := r.rdb.HDel(ctx, radarMetricsMemoryKey, field).Err(); err != nil {
			r.metrics.RecordRedis("cache_size", "write", err)
			return 0, false, err
		}
		r.metrics.RecordRedis("cache_size", "write", nil)
		return 0, false, nil
	}
	if err != nil {
		r.metrics.RecordRedis("cache_size", "read", err)
		return 0, false, err
	}
	r.metrics.RecordRedis("cache_size", "read", nil)
	if err := r.rdb.HSet(ctx, radarMetricsMemoryKey, field, usage).Err(); err != nil {
		r.metrics.RecordRedis("cache_size", "write", err)
		return 0, false, err
	}
	r.metrics.RecordRedis("cache_size", "write", nil)
	return usage, true, nil
}

type radarMemoryLedgerWrite struct {
	field  string
	stored bool
	cmd    *redis.IntCmd
}

// refreshRadarQuotaMemoryLedger re-measures every active quota key on every
// metrics cycle. This prevents a stale shared ledger entry from surviving
// indefinitely after an append-side MEMORY/HSET failure, while retaining a
// bounded two-round-trip path that never scans quota history.
func (r *radarCacheRepository) refreshRadarQuotaMemoryLedger(ctx context.Context, activeBuckets []string) (map[string]struct{}, error) {
	validFields := make(map[string]struct{}, len(activeBuckets))
	if len(activeBuckets) == 0 {
		return validFields, nil
	}

	readPipe := r.rdb.Pipeline()
	readCommands := make([]*redis.IntCmd, len(activeBuckets))
	keys := make([]string, len(activeBuckets))
	for i, bucketKey := range activeBuckets {
		keys[i] = radarBucketRedisKey(bucketKey)
		readCommands[i] = readPipe.MemoryUsage(ctx, keys[i])
	}
	_, _ = readPipe.Exec(ctx)

	writes := make([]radarMemoryLedgerWrite, 0, len(activeBuckets))
	writePipe := r.rdb.Pipeline()
	var firstErr error
	for i, cmd := range readCommands {
		field := radarMetricsLedgerField("quota_bucket", keys[i])
		err := cmd.Err()
		switch {
		case err == nil:
			r.metrics.RecordRedis("cache_size", "read", nil)
			usage := cmd.Val()
			writes = append(writes, radarMemoryLedgerWrite{
				field:  field,
				stored: true,
				cmd:    writePipe.HSet(ctx, radarMetricsMemoryKey, field, usage),
			})
		case errors.Is(err, redis.Nil):
			r.metrics.RecordRedis("cache_size", "read", nil)
			writes = append(writes, radarMemoryLedgerWrite{
				field: field,
				cmd:   writePipe.HDel(ctx, radarMetricsMemoryKey, field),
			})
		default:
			r.metrics.RecordRedis("cache_size", "read", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if len(writes) > 0 {
		_, _ = writePipe.Exec(ctx)
		for _, write := range writes {
			err := write.cmd.Err()
			r.metrics.RecordRedis("cache_size", "write", err)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if write.stored {
				validFields[write.field] = struct{}{}
			}
		}
	}
	if firstErr != nil {
		return validFields, firstErr
	}
	return validFields, nil
}

func (r *radarCacheRepository) scanRadarSetBounded(ctx context.Context, key string) ([]string, error) {
	values := make([]string, 0)
	seen := make(map[string]struct{})
	var cursor uint64
	for {
		page, next, err := r.rdb.SScan(ctx, key, cursor, "*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, value := range page {
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			values = append(values, value)
		}
		if len(seen) > radarMetricsMaxKeys {
			return nil, errors.New("radar metrics set exceeds key limit")
		}
		cursor = next
		if cursor == 0 {
			return values, nil
		}
	}
}

func (r *radarCacheRepository) scanRadarHashBounded(ctx context.Context, key string, maximum int) (map[string]string, error) {
	values := make(map[string]string)
	var cursor uint64
	for {
		page, next, err := r.rdb.HScan(ctx, key, cursor, "*", 100).Result()
		if err != nil {
			return nil, err
		}
		if len(page)%2 != 0 {
			return nil, errors.New("radar metrics hash scan returned invalid page")
		}
		for i := 0; i < len(page); i += 2 {
			values[page[i]] = page[i+1]
		}
		if len(values) > maximum {
			return nil, errors.New("radar metrics hash exceeds key limit")
		}
		cursor = next
		if cursor == 0 {
			return values, nil
		}
	}
}

func (r *radarCacheRepository) readRadarAggregatorMetricsState(ctx context.Context) (int, time.Time, time.Time, time.Time, time.Time, error) {
	stored, err := r.rdb.HMGet(
		ctx,
		radarMetricsStateKey,
		"run_version",
		"last_run_at",
		"last_success_at",
		"published_bucket_count",
		"next_fire_at",
	).Result()
	if err != nil {
		r.metrics.RecordRedis("aggregator_state", "read", err)
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, err
	}
	r.metrics.RecordRedis("aggregator_state", "read", nil)
	if len(stored) != 5 {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator metrics response")
	}
	if stored[0] == nil && stored[1] == nil && stored[2] == nil && stored[3] == nil && stored[4] == nil {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, nil
	}
	version, versionOK := stored[0].(string)
	lastRunRaw, runOK := stored[1].(string)
	if !versionOK || !runOK {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator metrics state")
	}
	parsedVersion, err := strconv.ParseInt(version, 10, 64)
	if err != nil || parsedVersion <= 0 {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator metrics version")
	}
	lastRun, err := time.Parse(time.RFC3339Nano, lastRunRaw)
	if err != nil || lastRun.IsZero() {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator last run")
	}
	lastAttempt := time.UnixMilli(parsedVersion).UTC()
	if lastAttempt.IsZero() || lastAttempt.After(lastRun) {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator last attempt")
	}
	var nextFire time.Time
	if nextRaw, ok := stored[4].(string); ok {
		nextFire, err = time.Parse(time.RFC3339Nano, nextRaw)
		if err != nil || nextFire.IsZero() {
			return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator next fire")
		}
	} else if stored[4] != nil {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator next fire")
	}
	lastSuccessRaw, successOK := stored[2].(string)
	bucketRaw, bucketOK := stored[3].(string)
	if successOK != bucketOK {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("incomplete radar aggregator success state")
	}
	if !successOK {
		return 0, lastAttempt, lastRun.UTC(), time.Time{}, nextFire.UTC(), nil
	}
	lastSuccess, err := time.Parse(time.RFC3339Nano, lastSuccessRaw)
	if err != nil || lastSuccess.IsZero() || lastSuccess.After(lastRun) {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator last success")
	}
	bucketCount, err := strconv.Atoi(bucketRaw)
	if err != nil || bucketCount < 0 {
		return 0, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid radar aggregator bucket count")
	}
	return bucketCount, lastAttempt, lastRun.UTC(), lastSuccess.UTC(), nextFire.UTC(), nil
}

// GetRadarAggregatorState reads only the shared, bounded aggregation ledger.
// Public source metadata uses this narrow path so it cannot fail or become
// expensive because of unrelated source, bucket, or memory-accounting state.
func (r *radarCacheRepository) GetRadarAggregatorState(ctx context.Context) (service.RadarMetricsSnapshot, error) {
	result := service.RadarMetricsSnapshot{}
	publishedCount, lastAttemptAt, lastRunAt, lastSuccessAt, nextFireAt, err := r.readRadarAggregatorMetricsState(ctx)
	if err != nil {
		return result, errors.New("read radar aggregator metrics state failed")
	}
	result.PublishedBucketCount = publishedCount
	result.AggregatorLastAttemptAt = lastAttemptAt
	result.AggregatorLastRunAt = lastRunAt
	result.AggregatorLastSuccessAt = lastSuccessAt
	result.AggregatorNextFireAt = nextFireAt
	result.AggregatorStateValid = true
	return result, nil
}

// GetRadarMetricsSnapshot reads a shared, bounded Redis accounting ledger.
// Every active quota key is re-measured using Redis MEMORY USAGE's bounded
// default sampling so transient ledger failures self-heal; history is never read.
func (r *radarCacheRepository) GetRadarMetricsSnapshot(ctx context.Context) (service.RadarMetricsSnapshot, error) {
	result, err := r.GetRadarAggregatorState(ctx)
	result.CacheMemoryBytes = map[string]int{}
	result.SourceLastSuccess = map[service.RadarSourceKey]time.Time{}
	if err != nil {
		return result, err
	}

	meta, err := r.scanRadarHashBounded(ctx, radarSourceMetaKey, radarMetricsMaxKeys)
	if err != nil {
		return result, errors.New("read radar metrics snapshot failed")
	}
	sources := make(map[service.RadarSourceKey]struct{}, len(r.metricsSources)+len(meta))
	for _, source := range r.metricsSources {
		sources[source] = struct{}{}
	}
	for rawSource := range meta {
		source := service.RadarSourceKey(rawSource)
		if _, err := radarSourceRedisKey(source); err != nil {
			return result, errors.New("read radar metrics snapshot found invalid source")
		}
		sources[source] = struct{}{}
	}
	for rawSource, encoded := range meta {
		source := service.RadarSourceKey(rawSource)
		var sourceMeta service.SourceFetchMeta
		if err := json.Unmarshal([]byte(encoded), &sourceMeta); err != nil || validateSourceFetchMeta(sourceMeta) != nil {
			return result, errors.New("read radar metrics snapshot found invalid source metadata")
		}
		if sourceMeta.LastSuccessAt != nil && !sourceMeta.LastSuccessAt.IsZero() {
			result.SourceLastSuccess[source] = sourceMeta.LastSuccessAt.UTC()
		}
	}

	bucketKeys, err := r.scanRadarSetBounded(ctx, radarBucketIndexKey)
	if err != nil {
		r.metrics.RecordRedis("cache_size", "read", err)
		result.Partial = true
		return result, nil
	}
	activeBuckets := make([]string, 0, len(bucketKeys))
	pipe := r.rdb.Pipeline()
	exists := make([]*redis.IntCmd, len(bucketKeys))
	for i, bucketKey := range bucketKeys {
		if _, _, err := service.ParseRadarBucketKey(bucketKey); err != nil {
			result.Partial = true
			return result, nil
		}
		exists[i] = pipe.Exists(ctx, radarBucketRedisKey(bucketKey))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		result.Partial = true
		return result, nil
	}
	missingBuckets := make([]string, 0)
	for i, bucketKey := range bucketKeys {
		if exists[i].Val() == 0 {
			missingBuckets = append(missingBuckets, bucketKey)
		} else {
			activeBuckets = append(activeBuckets, bucketKey)
		}
	}
	if err := r.cleanupMissingBucketIndexEntries(ctx, missingBuckets); err != nil {
		result.Partial = true
		return result, nil
	}
	result.ActiveBucketCount = len(activeBuckets)

	validFields, err := r.refreshRadarQuotaMemoryLedger(ctx, activeBuckets)
	if err != nil {
		result.Partial = true
		return result, nil
	}
	for source := range sources {
		key, err := radarSourceRedisKey(source)
		if err != nil {
			return result, errors.New("read radar metrics snapshot found invalid source")
		}
		family := radarMetricsCacheFamily(source)
		field := radarMetricsLedgerField(family, key)
		if _, stored, err := r.writeRadarMemoryLedgerEntry(ctx, family, key); err != nil {
			result.Partial = true
			return result, nil
		} else if stored {
			validFields[field] = struct{}{}
		}
	}
	for _, key := range []string{radarSourceMetaKey, radarSourceVersionKey, radarSourceCadenceKey, radarSourceCadenceVersionKey, radarBucketIndexKey, radarMetricsStateKey} {
		field := radarMetricsLedgerField("metadata", key)
		if _, stored, err := r.writeRadarMemoryLedgerEntry(ctx, "metadata", key); err != nil {
			result.Partial = true
			return result, nil
		} else if stored {
			validFields[field] = struct{}{}
		}
	}
	ledger, err := r.scanRadarHashBounded(ctx, radarMetricsMemoryKey, radarMetricsMaxLedger)
	if err != nil {
		result.Partial = true
		return result, nil
	}
	staleFields := make([]string, 0)
	for field, rawBytes := range ledger {
		family, valid := parseRadarMetricsLedgerField(field)
		_, current := validFields[field]
		if !valid || !current {
			staleFields = append(staleFields, field)
			continue
		}
		bytes, err := strconv.Atoi(rawBytes)
		if err != nil || bytes < 0 {
			result.Partial = true
			return result, nil
		}
		result.CacheMemoryBytes[family] += bytes
	}
	if len(staleFields) > 0 {
		if err := r.rdb.HDel(ctx, radarMetricsMemoryKey, staleFields...).Err(); err != nil {
			r.metrics.RecordRedis("cache_size", "write", err)
			result.Partial = true
			return result, nil
		}
		r.metrics.RecordRedis("cache_size", "write", nil)
	}
	result.CacheMemoryValid = true
	r.metrics.RecordRedis("cache_size", "read", nil)
	return result, nil
}

func validateRadarLockIdentity(task, owner string) error {
	if !isSafeRadarTask(task) {
		return fmt.Errorf("%w: invalid task token", service.ErrInvalidRadarCacheKey)
	}
	if strings.TrimSpace(owner) == "" {
		return errors.New("radar lock owner must not be blank")
	}
	return nil
}

func isSafeRadarTask(value string) bool {
	const performancePrefix = "aa_perf:"
	if strings.HasPrefix(value, performancePrefix) {
		source := service.RadarSourceKey(value)
		canonical, err := service.RadarAAPerformanceSource(strings.TrimPrefix(value, performancePrefix))
		return err == nil && canonical == source
	}
	if len(value) == 0 || len(value) > 128 || !isLowerAlphaNumericByte(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isLowerAlphaNumericByte(value[i]) && value[i] != ':' && value[i] != '_' && value[i] != '-' {
			return false
		}
	}
	return true
}

func isLowerAlphaNumericByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func validateRadarSnapshot(snapshot service.BucketSnapshotDTO) error {
	platform, planTier, err := service.ParseRadarBucketKey(snapshot.BucketKey)
	if err != nil {
		return err
	}
	if snapshot.Platform != platform || snapshot.PlanTier != planTier {
		return fmt.Errorf("%w: snapshot bucket fields do not match bucket key", service.ErrInvalidRadarCacheKey)
	}
	if snapshot.CapturedAt.IsZero() {
		return errors.New("radar snapshot captured_at is required")
	}
	return service.ValidateRadarBucketSnapshot(snapshot)
}

func decodeRadarSnapshot(encoded, bucketKey string) (service.BucketSnapshotDTO, error) {
	var snapshot service.BucketSnapshotDTO
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return service.BucketSnapshotDTO{}, errInvalidStoredRadarBucketSnapshot
	}
	if err := validateRadarSnapshot(snapshot); err != nil {
		return service.BucketSnapshotDTO{}, errInvalidStoredRadarBucketSnapshot
	}
	if snapshot.BucketKey != bucketKey {
		return service.BucketSnapshotDTO{}, errInvalidStoredRadarBucketSnapshot
	}
	return snapshot, nil
}

func radarBucketRedisKey(bucketKey string) string {
	return radarBucketKeyPrefix + bucketKey
}

func (r *radarCacheRepository) validateSourcePayloadWrite(
	source service.RadarSourceKey,
	payload []byte,
	retention time.Duration,
) (string, error) {
	key, err := radarSourceRedisKey(source)
	if err != nil {
		return "", err
	}
	if payload == nil {
		return "", errors.New("radar source payload must not be nil")
	}
	if int64(len(payload)) > r.externalResponseMaxBytes {
		return "", errors.New("radar source payload exceeds configured maximum")
	}
	if retention <= 0 {
		return "", errors.New("radar source retention must be positive")
	}
	if retention > r.sourceHardRetentionWindow {
		return "", errors.New("radar source retention exceeds configured hard limit")
	}
	return key, nil
}

func durationMillisecondsCeil(value time.Duration) int64 {
	milliseconds := value / time.Millisecond
	if value%time.Millisecond != 0 {
		milliseconds++
	}
	return int64(milliseconds)
}

func radarSourceRedisKey(source service.RadarSourceKey) (string, error) {
	switch source {
	case service.RadarSourceAA:
		return radarAASourceKey, nil
	case service.RadarSourceLMArena:
		return radarLMArenaSourceKey, nil
	case service.RadarSourceStatusClaude:
		return radarClaudeHealthKey, nil
	case service.RadarSourceStatusOpenAI:
		return radarOpenAIHealthKey, nil
	case service.RadarSourceStatusWindsurf:
		return radarWindsurfHealthKey, nil
	case service.RadarSourceStatusDeepSeek:
		return radarDeepSeekHealthKey, nil
	case service.RadarSourceStatusKimi:
		return radarKimiHealthKey, nil
	case service.RadarSourceStatusMiniMaxGlobal:
		return radarMiniMaxGlobalHealthKey, nil
	case service.RadarSourceStatusMiniMaxChina:
		return radarMiniMaxChinaHealthKey, nil
	}

	const performancePrefix = "aa_perf:"
	raw := string(source)
	if !strings.HasPrefix(raw, performancePrefix) {
		return "", fmt.Errorf("%w: unknown radar source", service.ErrInvalidRadarCacheKey)
	}
	modelSlug := strings.TrimPrefix(raw, performancePrefix)
	canonical, err := service.RadarAAPerformanceSource(modelSlug)
	if err != nil || canonical != source {
		return "", fmt.Errorf("%w: invalid Artificial Analysis performance source", service.ErrInvalidRadarCacheKey)
	}
	return radarAAPerfKeyPrefix + modelSlug, nil
}

func validateSourceFetchMeta(meta service.SourceFetchMeta) error {
	if meta.LastAttemptAt.IsZero() {
		return fmt.Errorf("radar source metadata last_attempt_at is required")
	}
	hasDeadline := meta.NextFireAt != nil
	hasVersion := meta.CadenceVersion != ""
	if hasVersion && !hasDeadline {
		return errors.New("radar source metadata cadence is incomplete")
	}
	if hasVersion {
		if err := validateRadarSourceCadence(service.RadarSourceCadence{
			NextFireAt: *meta.NextFireAt,
			Version:    meta.CadenceVersion,
		}); err != nil {
			return err
		}
	}
	if meta.Error == nil {
		return nil
	}
	switch *meta.Error {
	case service.DataSourceErrorCodeNetworkError,
		service.DataSourceErrorCodeUnauthorized,
		service.DataSourceErrorCodeRateLimited,
		service.DataSourceErrorCodeInvalidResponse,
		service.DataSourceErrorCodeUpstreamError:
		return nil
	default:
		return fmt.Errorf("radar source metadata contains an invalid error code")
	}
}

func validateRadarSourceCadence(cadence service.RadarSourceCadence) error {
	if cadence.NextFireAt.IsZero() {
		return errors.New("radar source next fire is required")
	}
	version, err := strconv.ParseInt(cadence.Version, 10, 64)
	if err != nil || version <= 0 || version > radarMaxExactCadenceVersion || strconv.FormatInt(version, 10) != cadence.Version {
		return errors.New("radar source cadence version is invalid")
	}
	return nil
}
