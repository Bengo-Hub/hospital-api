-- Modify "patients" table
ALTER TABLE "patients" ADD COLUMN "identification_type" character varying NULL, ADD COLUMN "sha_beneficiary_number" character varying NULL, ADD COLUMN "photo_url" character varying NULL, ADD COLUMN "household_id" uuid NULL;
