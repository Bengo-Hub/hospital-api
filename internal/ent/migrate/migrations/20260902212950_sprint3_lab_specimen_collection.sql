-- Modify "lab_order_lines" table
ALTER TABLE "lab_order_lines" ADD COLUMN "specimen_collected_at" timestamptz NULL, ADD COLUMN "specimen_collected_by" uuid NULL, ADD COLUMN "specimen_id" character varying NULL;
