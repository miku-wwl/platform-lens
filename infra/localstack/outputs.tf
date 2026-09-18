output "dynamodb_table" {
  value = aws_dynamodb_table.runs.name
}

output "s3_bucket" {
  value = aws_s3_bucket.artifacts.bucket
}

output "worker_user_name" {
  value = aws_iam_user.worker.name
}

output "worker_access_key_id" {
  value     = aws_iam_access_key.worker.id
  sensitive = true
}

output "worker_secret_access_key" {
  value     = aws_iam_access_key.worker.secret
  sensitive = true
}
