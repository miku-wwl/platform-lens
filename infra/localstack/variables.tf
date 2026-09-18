variable "localstack_endpoint" {
  type    = string
  default = "http://localhost:4566"
}

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "dynamodb_table" {
  type    = string
  default = "platformlens-runs"
}

variable "dynamodb_gsi" {
  type    = string
  default = "candidate-index"
}

variable "s3_bucket" {
  type    = string
  default = "platformlens-artifacts"
}
