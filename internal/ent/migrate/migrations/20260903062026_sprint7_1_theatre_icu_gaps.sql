-- Create "operative_notes" table
CREATE TABLE "operative_notes" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "surgeon_id" uuid NULL, "procedure_performed" character varying NOT NULL, "findings" character varying NULL, "complications" character varying NULL, "estimated_blood_loss_ml" double precision NULL, "implants_used" character varying NULL, "specimens_sent" boolean NOT NULL DEFAULT false, "specimens_description" character varying NULL, "post_op_diagnosis" character varying NULL, "authored_by" uuid NULL, "authored_at" timestamptz NOT NULL, "theatre_booking_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "operative_notes_theatre_bookings_operative_note" FOREIGN KEY ("theatre_booking_id") REFERENCES "theatre_bookings" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "operative_notes_theatre_booking_id_key" to table: "operative_notes"
CREATE UNIQUE INDEX "operative_notes_theatre_booking_id_key" ON "operative_notes" ("theatre_booking_id");
-- Create index "operativenote_tenant_id_theatre_booking_id" to table: "operative_notes"
CREATE UNIQUE INDEX "operativenote_tenant_id_theatre_booking_id" ON "operative_notes" ("tenant_id", "theatre_booking_id");
-- Create "pacu_stays" table
CREATE TABLE "pacu_stays" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "bay_label" character varying NULL, "admitted_at" timestamptz NOT NULL, "discharged_at" timestamptz NULL, "discharge_disposition" character varying NULL, "monitoring_notes" character varying NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "theatre_booking_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "pacu_stays_theatre_bookings_pacu_stays" FOREIGN KEY ("theatre_booking_id") REFERENCES "theatre_bookings" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "pacustay_tenant_id_theatre_booking_id" to table: "pacu_stays"
CREATE INDEX "pacustay_tenant_id_theatre_booking_id" ON "pacu_stays" ("tenant_id", "theatre_booking_id");
-- Create "theatre_staff_assignments" table
CREATE TABLE "theatre_staff_assignments" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "staff_user_id" uuid NOT NULL, "role" character varying NOT NULL, "assigned_at" timestamptz NOT NULL, "theatre_booking_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "theatre_staff_assignments_theatre_bookings_staff_assignments" FOREIGN KEY ("theatre_booking_id") REFERENCES "theatre_bookings" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "theatrestaffassignment_tenant_id_staff_user_id" to table: "theatre_staff_assignments"
CREATE INDEX "theatrestaffassignment_tenant_id_staff_user_id" ON "theatre_staff_assignments" ("tenant_id", "staff_user_id");
-- Create index "theatrestaffassignment_tenant_id_theatre_booking_id" to table: "theatre_staff_assignments"
CREATE INDEX "theatrestaffassignment_tenant_id_theatre_booking_id" ON "theatre_staff_assignments" ("tenant_id", "theatre_booking_id");
