FROM python:3.12.10-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt && useradd -u 10001 -m monitor
COPY app.py .
USER 10001
EXPOSE 8080
CMD ["python", "app.py"]
