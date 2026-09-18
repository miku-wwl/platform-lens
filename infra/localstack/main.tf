terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region                      = var.region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    dynamodb = var.localstack_endpoint
    s3       = var.localstack_endpoint
  }
}

resource "aws_dynamodb_table" "runs" {
  name         = var.dynamodb_table
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "run_id"

  attribute {
    name = "run_id"
    type = "S"
  }

  attribute {
    name = "state"
    type = "S"
  }

  attribute {
    name = "lease_expires_at"
    type = "S"
  }

  global_secondary_index {
    name            = var.dynamodb_gsi
    hash_key        = "state"
    range_key       = "lease_expires_at"
    projection_type = "ALL"
  }
}

resource "aws_s3_bucket" "artifacts" {
  bucket        = var.s3_bucket
  force_destroy = false
}
