-- Modify "examination_records" table
ALTER TABLE "examination_records" ADD COLUMN "diagnosis_history" jsonb NULL, ADD COLUMN "review_of_systems" jsonb NULL, ADD COLUMN "physical_exam_findings" jsonb NULL, ADD COLUMN "treatment_plan" character varying NULL;
