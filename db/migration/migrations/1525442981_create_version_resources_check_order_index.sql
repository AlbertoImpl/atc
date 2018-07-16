-- +goose Up
-- +goose StatementBegin
BEGIN;
  CREATE INDEX versioned_resources_check_order ON versioned_resources (check_order DESC);
COMMIT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
BEGIN;
  DROP INDEX versioned_resources_check_order;
COMMIT;
-- +goose StatementEnd
