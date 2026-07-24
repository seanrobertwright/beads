-- Recompute issues.is_blocked with a NULL-safe waits-for gate predicate.
--
-- Migration 0047 (and runtime recompute before this release) evaluated
-- JSON_EXTRACT(d.metadata, '$.gate') = 'any-children' directly. Waits-for
-- dependencies created without gate metadata (e.g. 'bd dep add --type
-- waits-for' stores no metadata) yield NULL there, and NULL poisons the
-- enclosing NOT(... AND ...) so the waiter was computed unblocked as soon
-- as any child closed. COALESCE to the all-children default (matching
-- internal/storage/issueops/blocked_state.go) and re-run the recompute so
-- rows mis-set by 0047 are repaired. The wisps-side twin is
-- ignored/0015_recompute_null_gate_wisp_is_blocked.up.sql.
--
-- The recompute joins the clone-local wisps/wisp_dependencies tables when
-- they exist. Those are dolt-ignored and are NOT present during the
-- main-source migration pass on a freshly materialized (baseline/remote-backed)
-- clone — but issues/dependencies are dolt-versioned, so a fresh clone can
-- still carry rows mis-set by 0047 and the issues repair must not be skipped
-- there. Run the full issue+wisp recompute when the wisp tables exist and a
-- wisp-free variant (issues/dependencies only) when they don't; ignored/0015
-- repairs the wisp rows once the clone-local tables materialize.
-- The full variant queries both wisps and wisp_dependencies, so require both
-- to exist before selecting it — a half-materialized wisp bootstrap must not
-- wedge the migration pass on a missing table.
SET @has_wisps = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME IN ('wisps', 'wisp_dependencies')
);

-- Existence is not enough: the full variant references wisp_dependencies'
-- post-split target columns (depends_on_issue_id / depends_on_wisp_id, split
-- by ignored/0003). Clone-local wisp tables created by an old binary can
-- still carry the legacy depends_on_id shape while the synced main cursor is
-- current, and the main-source pass runs before the ignored pass — so
-- selecting the full variant there fails on an unknown column before
-- ignored/0003 ever gets to split the table (the same hazard 0047 guards
-- with a column-shape check). Require the split shape too; the wisp-free
-- variant still repairs the issues rows, and ignored/0015 repairs the wisp
-- rows once the clone-local chain catches up.
SET @has_split_wisp_deps = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisp_dependencies'
      AND COLUMN_NAME IN ('depends_on_issue_id', 'depends_on_wisp_id')
);

-- Self-assign updated_at: is_blocked is derived state and issues.updated_at
-- carries ON UPDATE CURRENT_TIMESTAMP; letting the recompute bump it plants
-- per-clone wall clock in a synced table (see blocked_state.go, bd-578h9.19).
UPDATE issues SET is_blocked = 0, updated_at = updated_at;

SET @sql = IF(@has_wisps > 1 AND @has_split_wisp_deps > 1,
'WITH RECURSIVE
  directly_blocked(kind, id) AS (
    SELECT DISTINCT ''issue'', i.id
    FROM issues i
    WHERE i.status NOT IN (''closed'', ''pinned'')
      AND (
        EXISTS (
          SELECT 1
          FROM dependencies d
          JOIN issues t ON t.id = d.depends_on_issue_id
          WHERE d.issue_id = i.id
            AND d.type IN (''blocks'', ''conditional-blocks'')
            AND t.status NOT IN (''closed'', ''pinned'')
        )
        OR EXISTS (
          SELECT 1
          FROM dependencies d
          JOIN wisps t ON t.id = d.depends_on_wisp_id
          WHERE d.issue_id = i.id
            AND d.type IN (''blocks'', ''conditional-blocks'')
            AND t.status NOT IN (''closed'', ''pinned'')
        )
        OR EXISTS (
          SELECT 1
          FROM dependencies d
          WHERE d.issue_id = i.id
            AND d.type = ''waits-for''
            AND (
              EXISTS (
                SELECT 1
                FROM dependencies cd
                JOIN issues child ON child.id = cd.issue_id
                WHERE cd.type = ''parent-child''
                  AND (
                    (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                    OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                  )
                  AND child.status NOT IN (''closed'', ''pinned'')
              )
              OR EXISTS (
                SELECT 1
                FROM wisp_dependencies cd
                JOIN wisps child ON child.id = cd.issue_id
                WHERE cd.type = ''parent-child''
                  AND (
                    (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                    OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                  )
                  AND child.status NOT IN (''closed'', ''pinned'')
              )
            )
            AND NOT (
              COALESCE(JSON_UNQUOTE(JSON_EXTRACT(d.metadata, ''$.gate'')), ''all-children'') = ''any-children''
              AND (
                EXISTS (
                  SELECT 1
                  FROM dependencies cd
                  JOIN issues child ON child.id = cd.issue_id
                  WHERE cd.type = ''parent-child''
                    AND (
                      (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                      OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                    )
                    AND child.status = ''closed''
                )
                OR EXISTS (
                  SELECT 1
                  FROM wisp_dependencies cd
                  JOIN wisps child ON child.id = cd.issue_id
                  WHERE cd.type = ''parent-child''
                    AND (
                      (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                      OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                    )
                    AND child.status = ''closed''
                )
              )
            )
        )
      )
    UNION
    SELECT DISTINCT ''wisp'', w.id
    FROM wisps w
    WHERE w.status NOT IN (''closed'', ''pinned'')
      AND (
        EXISTS (
          SELECT 1
          FROM wisp_dependencies d
          JOIN issues t ON t.id = d.depends_on_issue_id
          WHERE d.issue_id = w.id
            AND d.type IN (''blocks'', ''conditional-blocks'')
            AND t.status NOT IN (''closed'', ''pinned'')
        )
        OR EXISTS (
          SELECT 1
          FROM wisp_dependencies d
          JOIN wisps t ON t.id = d.depends_on_wisp_id
          WHERE d.issue_id = w.id
            AND d.type IN (''blocks'', ''conditional-blocks'')
            AND t.status NOT IN (''closed'', ''pinned'')
        )
        OR EXISTS (
          SELECT 1
          FROM wisp_dependencies d
          WHERE d.issue_id = w.id
            AND d.type = ''waits-for''
            AND (
              EXISTS (
                SELECT 1
                FROM dependencies cd
                JOIN issues child ON child.id = cd.issue_id
                WHERE cd.type = ''parent-child''
                  AND (
                    (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                    OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                  )
                  AND child.status NOT IN (''closed'', ''pinned'')
              )
              OR EXISTS (
                SELECT 1
                FROM wisp_dependencies cd
                JOIN wisps child ON child.id = cd.issue_id
                WHERE cd.type = ''parent-child''
                  AND (
                    (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                    OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                  )
                  AND child.status NOT IN (''closed'', ''pinned'')
              )
            )
            AND NOT (
              COALESCE(JSON_UNQUOTE(JSON_EXTRACT(d.metadata, ''$.gate'')), ''all-children'') = ''any-children''
              AND (
                EXISTS (
                  SELECT 1
                  FROM dependencies cd
                  JOIN issues child ON child.id = cd.issue_id
                  WHERE cd.type = ''parent-child''
                    AND (
                      (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                      OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                    )
                    AND child.status = ''closed''
                )
                OR EXISTS (
                  SELECT 1
                  FROM wisp_dependencies cd
                  JOIN wisps child ON child.id = cd.issue_id
                  WHERE cd.type = ''parent-child''
                    AND (
                      (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                      OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                    )
                    AND child.status = ''closed''
                )
              )
            )
        )
      )
  ),
  reachable(kind, id) AS (
    SELECT kind, id FROM directly_blocked
    UNION
    SELECT ''issue'', d.issue_id
    FROM reachable r
    JOIN dependencies d
      ON d.type = ''parent-child''
     AND (
       (r.kind = ''issue'' AND d.depends_on_issue_id = r.id)
       OR (r.kind = ''wisp'' AND d.depends_on_wisp_id = r.id)
     )
    JOIN issues child ON child.id = d.issue_id
    WHERE child.status NOT IN (''closed'', ''pinned'')
    UNION
    SELECT ''wisp'', d.issue_id
    FROM reachable r
    JOIN wisp_dependencies d
      ON d.type = ''parent-child''
     AND (
       (r.kind = ''issue'' AND d.depends_on_issue_id = r.id)
       OR (r.kind = ''wisp'' AND d.depends_on_wisp_id = r.id)
     )
    JOIN wisps child ON child.id = d.issue_id
    WHERE child.status NOT IN (''closed'', ''pinned'')
  )
UPDATE issues
SET is_blocked = 1, updated_at = updated_at
WHERE id IN (SELECT id FROM reachable WHERE kind = ''issue'')
  AND status NOT IN (''closed'', ''pinned'')',
'WITH RECURSIVE
  directly_blocked(id) AS (
    SELECT DISTINCT i.id
    FROM issues i
    WHERE i.status NOT IN (''closed'', ''pinned'')
      AND (
        EXISTS (
          SELECT 1
          FROM dependencies d
          JOIN issues t ON t.id = d.depends_on_issue_id
          WHERE d.issue_id = i.id
            AND d.type IN (''blocks'', ''conditional-blocks'')
            AND t.status NOT IN (''closed'', ''pinned'')
        )
        OR EXISTS (
          SELECT 1
          FROM dependencies d
          WHERE d.issue_id = i.id
            AND d.type = ''waits-for''
            AND EXISTS (
              SELECT 1
              FROM dependencies cd
              JOIN issues child ON child.id = cd.issue_id
              WHERE cd.type = ''parent-child''
                AND (
                  (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                  OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                )
                AND child.status NOT IN (''closed'', ''pinned'')
            )
            AND NOT (
              COALESCE(JSON_UNQUOTE(JSON_EXTRACT(d.metadata, ''$.gate'')), ''all-children'') = ''any-children''
              AND EXISTS (
                SELECT 1
                FROM dependencies cd
                JOIN issues child ON child.id = cd.issue_id
                WHERE cd.type = ''parent-child''
                  AND (
                    (d.depends_on_issue_id IS NOT NULL AND cd.depends_on_issue_id = d.depends_on_issue_id)
                    OR (d.depends_on_wisp_id IS NOT NULL AND cd.depends_on_wisp_id = d.depends_on_wisp_id)
                  )
                  AND child.status = ''closed''
              )
            )
        )
      )
  ),
  reachable(id) AS (
    SELECT id FROM directly_blocked
    UNION
    SELECT d.issue_id
    FROM reachable r
    JOIN dependencies d
      ON d.type = ''parent-child''
     AND d.depends_on_issue_id = r.id
    JOIN issues child ON child.id = d.issue_id
    WHERE child.status NOT IN (''closed'', ''pinned'')
  )
UPDATE issues
SET is_blocked = 1, updated_at = updated_at
WHERE id IN (SELECT id FROM reachable)
  AND status NOT IN (''closed'', ''pinned'')');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
