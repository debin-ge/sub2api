-- Before upstream 239 consumes the legacy group JSON key, preserve max when
-- the new map configures other efforts only. Explicit new max always wins.
-- This is a one-time conversion; the original migration remains checksum-stable.
UPDATE groups AS g
SET model_pricing = (
    SELECT jsonb_agg(
        CASE
            WHEN jsonb_typeof(entry) = 'object'
                 AND jsonb_typeof(entry->'max_reasoning_effort_multiplier') = 'number'
                 AND jsonb_typeof(entry->'reasoning_effort_multipliers') = 'object'
                 AND NOT ((entry->'reasoning_effort_multipliers') ? 'max') THEN
                jsonb_set(entry, '{reasoning_effort_multipliers}',
                    (entry->'reasoning_effort_multipliers') ||
                    jsonb_build_object('max', entry->'max_reasoning_effort_multiplier'))
            ELSE entry
        END ORDER BY ordinal
    )
    FROM jsonb_array_elements(g.model_pricing) WITH ORDINALITY AS pricing(entry, ordinal)
)
WHERE jsonb_typeof(g.model_pricing) = 'array'
  AND g.model_pricing <> '[]'::jsonb;
