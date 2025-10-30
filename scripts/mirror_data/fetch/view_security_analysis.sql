CREATE VIEW security_analysis AS
SELECT
    value->>'name' AS name,
    sec_control,
    score,
    updated,
    review
FROM servers,
     UNNEST(ARRAY[
         ROW('authorAnalysis'::TEXT,
             (faun#>>'{authorAnalysis, score}')::INTEGER,
             (faun#>>'{authorAnalysis, updated}')::TEXT,
             (faun#>>'{authorAnalysis, review}')::TEXT),
         ROW('sourceCodeAnalysis'::TEXT,
             (faun#>>'{sourceCodeAnalysis, score}')::INTEGER,
             (faun#>>'{sourceCodeAnalysis, updated}')::TEXT,
             (faun#>>'{sourceCodeAnalysis, review}')::TEXT)
         ]) AS t(sec_control TEXT, score INTEGER, updated TEXT, review TEXT);