-- Create "walk_in_sales" table
CREATE TABLE "walk_in_sales" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "prescription_number" character varying NOT NULL, "sale_number" character varying NOT NULL, "patient_name" character varying NULL, "line_items" jsonb NULL, "amount" double precision NOT NULL, "status" character varying NOT NULL DEFAULT 'pending', "payment_method" character varying NULL, "treasury_invoice_id" uuid NULL, "treasury_payment_intent_id" uuid NULL, "etims_invoice_number" character varying NULL, "etims_qr_code_url" character varying NULL, "collected_by" uuid NULL, "created_by_user_id" uuid NULL, "paid_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "prescription_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "walk_in_sales_prescriptions_walk_in_sales" FOREIGN KEY ("prescription_id") REFERENCES "prescriptions" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "walkinsale_tenant_id_created_at" to table: "walk_in_sales"
CREATE INDEX "walkinsale_tenant_id_created_at" ON "walk_in_sales" ("tenant_id", "created_at");
-- Create index "walkinsale_tenant_id_prescription_id" to table: "walk_in_sales"
CREATE INDEX "walkinsale_tenant_id_prescription_id" ON "walk_in_sales" ("tenant_id", "prescription_id");
-- Create index "walkinsale_tenant_id_sale_number" to table: "walk_in_sales"
CREATE UNIQUE INDEX "walkinsale_tenant_id_sale_number" ON "walk_in_sales" ("tenant_id", "sale_number");
-- Create index "walkinsale_tenant_id_status" to table: "walk_in_sales"
CREATE INDEX "walkinsale_tenant_id_status" ON "walk_in_sales" ("tenant_id", "status");
