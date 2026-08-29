-- Create "document_sequences" table
CREATE TABLE "document_sequences" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "kind" character varying NOT NULL, "prefix" character varying NULL, "next_value" bigint NOT NULL DEFAULT 1, "pad_width" bigint NOT NULL DEFAULT 5, "format" character varying NULL, "reset_period" character varying NOT NULL DEFAULT 'none', "period_key" character varying NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "documentsequence_tenant_id_kind" to table: "document_sequences"
CREATE UNIQUE INDEX "documentsequence_tenant_id_kind" ON "document_sequences" ("tenant_id", "kind");
-- Create "outbox_events" table
CREATE TABLE "outbox_events" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "aggregate_type" character varying NOT NULL, "aggregate_id" character varying NOT NULL, "event_type" character varying NOT NULL, "payload" jsonb NOT NULL, "status" character varying NOT NULL DEFAULT 'PENDING', "attempts" bigint NOT NULL DEFAULT 0, "last_attempt_at" timestamptz NULL, "published_at" timestamptz NULL, "error_message" text NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "outboxevent_created_at" to table: "outbox_events"
CREATE INDEX "outboxevent_created_at" ON "outbox_events" ("created_at");
-- Create index "outboxevent_status" to table: "outbox_events"
CREATE INDEX "outboxevent_status" ON "outbox_events" ("status");
-- Create index "outboxevent_tenant_id_status" to table: "outbox_events"
CREATE INDEX "outboxevent_tenant_id_status" ON "outbox_events" ("tenant_id", "status");
