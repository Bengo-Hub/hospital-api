-- Modify "beds" table
ALTER TABLE "beds" ADD COLUMN "equipment_asset_ids" jsonb NULL;
-- Modify "icu_episodes" table
ALTER TABLE "icu_episodes" ADD COLUMN "equipment_asset_ids" jsonb NULL;
-- Modify "theatre_bookings" table
ALTER TABLE "theatre_bookings" ADD COLUMN "equipment_asset_ids" jsonb NULL;
