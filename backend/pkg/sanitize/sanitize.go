package sanitize
docker run --name ganji-postgres \
  -e POSTGRES_USER=student \
  -e POSTGRES_PASSWORD=devpassword \
  -e POSTGRES_DB=ganji \
  -p 5432:5432 \
  -d postgres:16