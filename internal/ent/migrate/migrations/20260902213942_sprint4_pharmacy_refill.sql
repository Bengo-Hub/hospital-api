-- Modify "prescriptions" table
ALTER TABLE "prescriptions" ADD COLUMN "repeat_of_prescription_id" uuid NULL;
-- Create index "prescription_tenant_id_repeat_of_prescription_id" to table: "prescriptions"
CREATE INDEX "prescription_tenant_id_repeat_of_prescription_id" ON "prescriptions" ("tenant_id", "repeat_of_prescription_id");
