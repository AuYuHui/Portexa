ALTER TABLE history_coverage ADD COLUMN checked_through_wall INTEGER
  CHECK (checked_through_wall > start_wall AND checked_through_wall <= end_wall);
