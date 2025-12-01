# NAV Prediction Service

Microservice untuk memprediksi NAV (Net Asset Value) reksadana menggunakan XGBoost Machine Learning.

## Fitur

- Prediksi NAV hingga 7 hari ke depan
- Menggunakan XGBoost dengan fitur technical analysis
- Support batch prediction untuk multiple mutual funds
- Integrasi dengan data historis dari Bareksa

## API Endpoints

### 1. Predict NAV untuk Single Mutual Fund

```
GET /mutual-funds/{id}/predict?days=3&history_days=365
```

**Parameters:**
- `id` (path): ID mutual fund dari database
- `days` (query, optional): Jumlah hari prediksi (default: 3, max: 7)
- `history_days` (query, optional): Jumlah hari data historis untuk training (default: 365)

**Response:**
```json
{
    "success": true,
    "mutual_fund": {
        "id": 1,
        "pid": 12345,
        "name": "Reksadana ABC"
    },
    "latest_nav": {
        "date": "2025-11-27",
        "value": 1234.56
    },
    "predictions": [
        {
            "date": "2025-11-28",
            "day": 1,
            "predicted_nav": 1235.12
        },
        {
            "date": "2025-11-29",
            "day": 2,
            "predicted_nav": 1236.45
        },
        {
            "date": "2025-12-01",
            "day": 3,
            "predicted_nav": 1237.89
        }
    ],
    "summary": {
        "average_predicted_nav": 1236.49,
        "trend": "up",
        "change_percent": 0.2698,
        "prediction_days": 3,
        "data_points_used": 250
    }
}
```

### 2. Batch Prediction

```
POST /mutual-funds/predict/batch?days=3
```

**Request Body:**
```json
[1, 2, 3, 4, 5]
```

**Response:**
```json
{
    "success": true,
    "count": 5,
    "results": [
        {
            "mutual_fund": {
                "id": 1,
                "name": "Reksadana ABC"
            },
            "predictions": [...],
            "summary": {...}
        },
        ...
    ]
}
```

### 3. Health Check

```
GET /prediction/health
```

**Response:**
```json
{
    "success": true,
    "prediction_service": "healthy"
}
```

## Technical Details

### Features Used for Prediction

Model XGBoost menggunakan fitur-fitur berikut:

1. **Lag Features**: NAV dari 1, 2, 3, 5, 7, 14, 30 hari sebelumnya
2. **Moving Averages**: MA 5, 10, 20, 30 hari
3. **Exponential Moving Averages**: EMA 5, 10, 20 hari
4. **Volatility**: Standard deviation 5, 10, 20 hari
5. **Returns**: Percentage change 1, 5, 10 hari
6. **Momentum**: Perubahan harga 5, 10 hari
7. **Rate of Change**: ROC 5, 10 hari
8. **Time Features**: Day of week, day of month, month

### Requirements

- Minimal 50 data points historis untuk training
- Data NAV harian dari Bareksa API

## Running Locally

### With Docker Compose

```bash
docker-compose up --build
```

### Standalone Python Service

```bash
cd prediction_service
pip install -r requirements.txt
python app.py
```

Service akan berjalan di `http://localhost:5001`

## Environment Variables

- `PREDICTION_SERVICE_URL`: URL prediction service (default: `http://prediction-service:5001`)

## Notes

- Prediksi akan melewati hari weekend (Sabtu-Minggu)
- Model di-train ulang setiap kali ada request prediksi untuk memastikan menggunakan data terbaru
- Maksimum prediksi adalah 7 hari ke depan
- Maksimum 10 mutual fund per batch request
