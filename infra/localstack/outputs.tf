output "dynamodb_table" {
  value = aws_dynamodb_table.runs.name
}

output "s3_bucket" {
  value = aws_s3_bucket.artifacts.bucket
}
