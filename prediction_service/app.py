from flask import Flask, request, jsonify
import numpy as np
import pandas as pd
import xgboost as xgb
from sklearn.preprocessing import MinMaxScaler, RobustScaler
from sklearn.model_selection import TimeSeriesSplit, GridSearchCV
from sklearn.ensemble import RandomForestRegressor, GradientBoostingRegressor
import requests
from datetime import datetime, timedelta
import logging
import warnings
warnings.filterwarnings('ignore')

app = Flask(__name__)
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class NAVPredictor:
    def __init__(self):
        self.model = None
        self.scaler = RobustScaler()  # More robust to outliers
        self.sequence_length = 30
        self.ensemble_models = {}
        self.use_ensemble = True
    
    def fetch_nav_data(self, pid, days=365):
        """Fetch NAV data from Bareksa API"""
        # Use cperiod format instead of date range
        cperiod = "1y"  # 1 year
        if days > 365:
            cperiod = "3y"
        elif days > 730:
            cperiod = "5y"
        
        url = f"https://www.bareksa.com/ajax/mutualfund/nav/product1/"
        params = {
            "id": pid,
            "cperiod": cperiod
        }
        
        headers = {
            "X-Requested-With": "XMLHttpRequest"
        }
        
        try:
            response = requests.get(url, params=params, headers=headers, timeout=10)
            response.raise_for_status()
            data = response.json()
            logger.info(f"Bareksa response status: {data.get('status')}")
            return data
        except Exception as e:
            logger.error(f"Error fetching NAV data: {e}")
            return None
    
    def prepare_data(self, nav_data):
        """Prepare data for XGBoost training"""
        if not nav_data:
            logger.error("No nav_data received")
            return None
        
        # Handle Bareksa format: data.datas[0].nav
        nav_list = None
        if 'data' in nav_data and 'datas' in nav_data['data']:
            datas = nav_data['data']['datas']
            if datas and len(datas) > 0 and 'nav' in datas[0]:
                nav_list = datas[0]['nav']
                logger.info(f"Found {len(nav_list)} NAV records from Bareksa format")
        elif 'data' in nav_data:
            nav_list = nav_data['data']
        
        if not nav_list:
            logger.error("No NAV data found in response")
            return None
        
        # Extract NAV values and dates
        records = []
        for item in nav_list:
            try:
                # Bareksa format: {"date": "2024-01-01", "value": "1234.56"}
                if isinstance(item, dict):
                    date = item.get('date') or item.get('tanggal')
                    nav = item.get('value') or item.get('nav') or item.get('nilai')
                    if date and nav:
                        records.append({
                            'date': pd.to_datetime(date),
                            'nav': float(nav)
                        })
                elif isinstance(item, list) and len(item) >= 2:
                    # Alternative format: [timestamp, nav]
                    records.append({
                        'date': pd.to_datetime(item[0], unit='ms') if isinstance(item[0], (int, float)) else pd.to_datetime(item[0]),
                        'nav': float(item[1])
                    })
            except Exception as e:
                logger.warning(f"Error parsing item {item}: {e}")
                continue
        
        logger.info(f"Parsed {len(records)} records")
        
        if len(records) < self.sequence_length + 1:
            logger.error(f"Not enough records: {len(records)} < {self.sequence_length + 1}")
            return None
        
        df = pd.DataFrame(records)
        df = df.sort_values('date').reset_index(drop=True)
        
        return df
    
    def create_features(self, df):
        """Create optimized features for prediction with comprehensive technical indicators"""
        df = df.copy()
        
        # ============ a. LAG RETURNS & LOG RETURNS ============
        # Lag Returns (pct_change)
        df['ret_1d'] = df['nav'].pct_change(1)
        df['ret_2d'] = df['nav'].pct_change(2)
        df['ret_3d'] = df['nav'].pct_change(3)
        df['ret_5d'] = df['nav'].pct_change(5)
        df['ret_7d'] = df['nav'].pct_change(7)
        df['ret_10d'] = df['nav'].pct_change(10)
        df['ret_14d'] = df['nav'].pct_change(14)
        df['ret_21d'] = df['nav'].pct_change(21)
        
        # Log Returns - better for financial time series
        df['log_ret_1d'] = np.log(df['nav']) - np.log(df['nav'].shift(1))
        df['log_ret_2d'] = np.log(df['nav']) - np.log(df['nav'].shift(2))
        df['log_ret_3d'] = np.log(df['nav']) - np.log(df['nav'].shift(3))
        df['log_ret_5d'] = np.log(df['nav']) - np.log(df['nav'].shift(5))
        df['log_ret_10d'] = np.log(df['nav']) - np.log(df['nav'].shift(10))
        
        # Cumulative Log Returns
        df['cum_log_ret_5d'] = df['log_ret_1d'].rolling(window=5).sum()
        df['cum_log_ret_10d'] = df['log_ret_1d'].rolling(window=10).sum()
        df['cum_log_ret_20d'] = df['log_ret_1d'].rolling(window=20).sum()
        
        # ============ LAG NAV FEATURES ============
        for lag in [1, 2, 3, 5, 7, 10, 14, 21]:
            df[f'nav_lag_{lag}'] = df['nav'].shift(lag)
        
        # ============ b. MOVING AVERAGE NAV & RETURN ============
        # Moving Average NAV
        df['ma_5'] = df['nav'].rolling(window=5).mean()
        df['ma_10'] = df['nav'].rolling(window=10).mean()
        df['ma_20'] = df['nav'].rolling(window=20).mean()
        df['ma_50'] = df['nav'].rolling(window=50).mean()
        
        # MA Ratio (NAV relative to MA)
        df['ma_5_ratio'] = df['nav'] / (df['ma_5'] + 1e-10)
        df['ma_10_ratio'] = df['nav'] / (df['ma_10'] + 1e-10)
        df['ma_20_ratio'] = df['nav'] / (df['ma_20'] + 1e-10)
        df['ma_50_ratio'] = df['nav'] / (df['ma_50'] + 1e-10)
        
        # Moving Average Crossover Signals
        df['ma_5_10_cross'] = df['ma_5'] - df['ma_10']
        df['ma_10_20_cross'] = df['ma_10'] - df['ma_20']
        df['ma_20_50_cross'] = df['ma_20'] - df['ma_50']
        
        # Return Moving Averages (Moving Average of Returns)
        df['ret_ma_5'] = df['ret_1d'].rolling(window=5).mean()
        df['ret_ma_10'] = df['ret_1d'].rolling(window=10).mean()
        df['ret_ma_20'] = df['ret_1d'].rolling(window=20).mean()
        
        # Log Return Moving Averages
        df['log_ret_ma_5'] = df['log_ret_1d'].rolling(window=5).mean()
        df['log_ret_ma_10'] = df['log_ret_1d'].rolling(window=10).mean()
        df['log_ret_ma_20'] = df['log_ret_1d'].rolling(window=20).mean()
        
        # ============ EXPONENTIAL MOVING AVERAGES ============
        for span in [5, 10, 12, 20, 26]:
            df[f'ema_{span}'] = df['nav'].ewm(span=span, adjust=False).mean()
        
        # EMA Ratios
        df['ema_5_ratio'] = df['nav'] / (df['ema_5'] + 1e-10)
        df['ema_10_ratio'] = df['nav'] / (df['ema_10'] + 1e-10)
        df['ema_20_ratio'] = df['nav'] / (df['ema_20'] + 1e-10)
        
        # ============ c. VOLATILITAS ============
        # Volatility based on daily returns (standard deviation)
        df['vol_5'] = df['ret_1d'].rolling(window=5).std()
        df['vol_10'] = df['ret_1d'].rolling(window=10).std()
        df['vol_14'] = df['ret_1d'].rolling(window=14).std()
        df['vol_20'] = df['ret_1d'].rolling(window=20).std()
        df['vol_30'] = df['ret_1d'].rolling(window=30).std()
        
        # Annualized Volatility (assuming 252 trading days)
        df['vol_10_annual'] = df['vol_10'] * np.sqrt(252)
        df['vol_20_annual'] = df['vol_20'] * np.sqrt(252)
        
        # Volatility of Log Returns
        df['log_vol_10'] = df['log_ret_1d'].rolling(window=10).std()
        df['log_vol_20'] = df['log_ret_1d'].rolling(window=20).std()
        
        # Volatility Ratio (short-term vs long-term)
        df['vol_ratio_5_20'] = df['vol_5'] / (df['vol_20'] + 1e-10)
        df['vol_ratio_10_20'] = df['vol_10'] / (df['vol_20'] + 1e-10)
        
        # Historical volatility using NAV (price volatility)
        df['nav_vol_10'] = df['nav'].rolling(window=10).std()
        df['nav_vol_20'] = df['nav'].rolling(window=20).std()
        
        # ============ d. MOMENTUM INDICATORS ============
        
        # --- RSI (Relative Strength Index) ---
        for period in [7, 14, 21]:
            delta = df['nav'].diff()
            gain = (delta.where(delta > 0, 0)).rolling(window=period).mean()
            loss = (-delta.where(delta < 0, 0)).rolling(window=period).mean()
            rs = gain / (loss + 1e-10)
            df[f'rsi_{period}'] = 100 - (100 / (1 + rs))
        
        # RSI Overbought/Oversold signals
        df['rsi_14_overbought'] = (df['rsi_14'] > 70).astype(int)
        df['rsi_14_oversold'] = (df['rsi_14'] < 30).astype(int)
        df['rsi_14_neutral'] = ((df['rsi_14'] >= 30) & (df['rsi_14'] <= 70)).astype(int)
        
        # --- MACD (Moving Average Convergence Divergence) ---
        ema_12 = df['nav'].ewm(span=12, adjust=False).mean()
        ema_26 = df['nav'].ewm(span=26, adjust=False).mean()
        df['macd'] = ema_12 - ema_26
        df['macd_signal'] = df['macd'].ewm(span=9, adjust=False).mean()
        df['macd_histogram'] = df['macd'] - df['macd_signal']
        
        # MACD Crossover Signals
        df['macd_bullish'] = ((df['macd'] > df['macd_signal']) & 
                              (df['macd'].shift(1) <= df['macd_signal'].shift(1))).astype(int)
        df['macd_bearish'] = ((df['macd'] < df['macd_signal']) & 
                              (df['macd'].shift(1) >= df['macd_signal'].shift(1))).astype(int)
        
        # Normalized MACD
        df['macd_normalized'] = df['macd'] / (df['nav'] + 1e-10) * 100
        
        # --- ROC (Rate of Change) ---
        for period in [3, 5, 7, 10, 14, 21]:
            df[f'roc_{period}'] = ((df['nav'] - df['nav'].shift(period)) / 
                                   (df['nav'].shift(period) + 1e-10)) * 100
        
        # --- Momentum (Price difference) ---
        for period in [3, 5, 7, 10, 14]:
            df[f'momentum_{period}'] = df['nav'] - df['nav'].shift(period)
        
        # --- Stochastic Oscillator ---
        for period in [14]:
            low_min = df['nav'].rolling(window=period).min()
            high_max = df['nav'].rolling(window=period).max()
            df[f'stoch_k_{period}'] = 100 * (df['nav'] - low_min) / (high_max - low_min + 1e-10)
            df[f'stoch_d_{period}'] = df[f'stoch_k_{period}'].rolling(window=3).mean()
        
        # --- Williams %R ---
        for period in [14]:
            high_max = df['nav'].rolling(window=period).max()
            low_min = df['nav'].rolling(window=period).min()
            df[f'williams_r_{period}'] = -100 * (high_max - df['nav']) / (high_max - low_min + 1e-10)
        
        # --- CCI (Commodity Channel Index) adapted for NAV ---
        for period in [20]:
            tp = df['nav']  # Using NAV as typical price
            tp_ma = tp.rolling(window=period).mean()
            tp_std = tp.rolling(window=period).std()
            df[f'cci_{period}'] = (tp - tp_ma) / (0.015 * tp_std + 1e-10)
        
        # ============ BOLLINGER BANDS ============
        for window in [10, 20]:
            ma = df['nav'].rolling(window=window).mean()
            std = df['nav'].rolling(window=window).std()
            df[f'bb_upper_{window}'] = ma + (std * 2)
            df[f'bb_lower_{window}'] = ma - (std * 2)
            df[f'bb_middle_{window}'] = ma
            bb_range = df[f'bb_upper_{window}'] - df[f'bb_lower_{window}']
            df[f'bb_position_{window}'] = (df['nav'] - df[f'bb_lower_{window}']) / (bb_range + 1e-10)
            df[f'bb_width_{window}'] = bb_range / (ma + 1e-10)  # Bandwidth
        
        # ============ PRICE RANGE / DONCHIAN CHANNEL ============
        for window in [5, 10, 14, 20]:
            df[f'high_{window}'] = df['nav'].rolling(window=window).max()
            df[f'low_{window}'] = df['nav'].rolling(window=window).min()
            range_val = df[f'high_{window}'] - df[f'low_{window}']
            df[f'channel_position_{window}'] = (df['nav'] - df[f'low_{window}']) / (range_val + 1e-10)
            df[f'channel_width_{window}'] = range_val / (df['nav'] + 1e-10)
        
        # ============ TREND STRENGTH ============
        df['trend_5'] = (df['nav'] - df['nav'].shift(5)) / (df['nav_vol_10'] + 1e-10)
        df['trend_10'] = (df['nav'] - df['nav'].shift(10)) / (df['nav_vol_20'] + 1e-10)
        df['trend_20'] = (df['nav'] - df['nav'].shift(20)) / (df['nav_vol_20'] + 1e-10)
        
        # ADX-like trend strength (simplified)
        df['trend_strength'] = abs(df['ret_ma_10']) / (df['vol_10'] + 1e-10)
        
        # ============ MEAN REVERSION INDICATORS ============
        # Z-Score (how many std devs from mean)
        df['zscore_20'] = (df['nav'] - df['ma_20']) / (df['nav_vol_20'] + 1e-10)
        df['zscore_50'] = (df['nav'] - df['ma_50']) / (df['nav'].rolling(50).std() + 1e-10)
        
        # Distance from moving averages
        df['dist_from_ma_10'] = (df['nav'] - df['ma_10']) / (df['ma_10'] + 1e-10) * 100
        df['dist_from_ma_20'] = (df['nav'] - df['ma_20']) / (df['ma_20'] + 1e-10) * 100
        
        # ============ TIME FEATURES ============
        if 'date' in df.columns:
            df['day_of_week'] = df['date'].dt.dayofweek
            df['day_of_month'] = df['date'].dt.day
            df['month'] = df['date'].dt.month
            df['week_of_year'] = df['date'].dt.isocalendar().week.astype(int)
            df['quarter'] = df['date'].dt.quarter
            
            # Cyclical encoding for periodic patterns
            df['day_sin'] = np.sin(2 * np.pi * df['day_of_week'] / 7)
            df['day_cos'] = np.cos(2 * np.pi * df['day_of_week'] / 7)
            df['month_sin'] = np.sin(2 * np.pi * df['month'] / 12)
            df['month_cos'] = np.cos(2 * np.pi * df['month'] / 12)
            df['dom_sin'] = np.sin(2 * np.pi * df['day_of_month'] / 31)
            df['dom_cos'] = np.cos(2 * np.pi * df['day_of_month'] / 31)
            
            # Is month end/start (mutual fund NAV patterns)
            df['is_month_start'] = (df['day_of_month'] <= 5).astype(int)
            df['is_month_end'] = (df['day_of_month'] >= 25).astype(int)
        
        return df
    
    def select_features(self, X, y, feature_cols, top_n=50):
        """Select top N most important features using XGBoost feature importance"""
        # Train a quick XGBoost model to get feature importance
        selector_model = xgb.XGBRegressor(
            n_estimators=100,
            max_depth=4,
            learning_rate=0.1,
            random_state=42,
            n_jobs=-1
        )
        selector_model.fit(X, y)
        
        # Get feature importances
        importances = selector_model.feature_importances_
        
        # Create DataFrame with feature names and importances
        importance_df = pd.DataFrame({
            'feature': feature_cols,
            'importance': importances
        }).sort_values('importance', ascending=False)
        
        # Select top N features
        selected_features = importance_df.head(top_n)['feature'].tolist()
        
        logger.info(f"Selected top {len(selected_features)} features from {len(feature_cols)}")
        logger.info(f"Top 10 features: {selected_features[:10]}")
        
        return selected_features
    
    def train_model(self, df):
        """Train ensemble model with feature selection and optimized hyperparameters"""
        df_features = self.create_features(df)
        df_features = df_features.dropna()
        
        if len(df_features) < 50:
            logger.warning("Not enough data for training")
            return False
        
        all_feature_cols = [col for col in df_features.columns if col not in ['date', 'nav']]
        
        X_all = df_features[all_feature_cols].values
        y = df_features['nav'].values
        
        # Handle infinite values
        X_all = np.nan_to_num(X_all, nan=0.0, posinf=0.0, neginf=0.0)
        
        # ============ FEATURE SELECTION ============
        # Select top 50 most important features to reduce overfitting
        selected_feature_cols = self.select_features(X_all, y, all_feature_cols, top_n=50)
        
        # Get indices of selected features
        selected_indices = [all_feature_cols.index(f) for f in selected_feature_cols]
        X = X_all[:, selected_indices]
        
        # Scale features
        X_scaled = self.scaler.fit_transform(X)
        
        # Use validation split for early stopping (85% train, 15% val)
        split_idx = int(len(X_scaled) * 0.85)
        X_train, X_val = X_scaled[:split_idx], X_scaled[split_idx:]
        y_train, y_val = y[:split_idx], y[split_idx:]
        
        # ============ OPTIMIZED XGBOOST (tuned for less overfitting) ============
        self.model = xgb.XGBRegressor(
            n_estimators=500,
            max_depth=4,  # Reduced depth
            learning_rate=0.01,  # Lower learning rate
            subsample=0.7,  # Less subsample
            colsample_bytree=0.7,  # Less column sampling
            min_child_weight=10,  # Higher min child weight
            gamma=0.2,  # Higher gamma for more regularization
            reg_alpha=1.0,  # L1 regularization
            reg_lambda=2.0,  # L2 regularization
            random_state=42,
            objective='reg:squarederror',
            early_stopping_rounds=50,
            eval_metric='mae'
        )
        
        self.model.fit(
            X_train, y_train,
            eval_set=[(X_val, y_val)],
            verbose=False
        )
        
        if self.use_ensemble:
            # ============ GRADIENT BOOSTING (tuned for less overfitting) ============
            self.ensemble_models['gb'] = GradientBoostingRegressor(
                n_estimators=300,
                max_depth=3,  # Reduced depth
                learning_rate=0.02,  # Lower learning rate
                subsample=0.7,
                min_samples_split=15,
                min_samples_leaf=10,
                max_features='sqrt',  # Use sqrt of features
                random_state=42
            )
            self.ensemble_models['gb'].fit(X_train, y_train)
            
            # ============ RANDOM FOREST (tuned for less overfitting) ============
            self.ensemble_models['rf'] = RandomForestRegressor(
                n_estimators=200,
                max_depth=6,  # Reduced depth
                min_samples_split=15,
                min_samples_leaf=10,
                max_features='sqrt',  # Use sqrt of features
                random_state=42,
                n_jobs=-1
            )
            self.ensemble_models['rf'].fit(X_train, y_train)
        
        # Store selected feature columns
        self.feature_cols = selected_feature_cols
        self.all_feature_cols = all_feature_cols
        self.selected_indices = selected_indices
        
        # Store training statistics for trend adjustment
        self.train_nav_mean = np.mean(y)
        self.train_nav_std = np.std(y)
        self.last_train_nav = y[-1]
        
        # Calculate recent trend from training data
        if len(y) >= 5:
            self.recent_returns = np.diff(y[-6:]) / y[-6:-1]  # Last 5 returns
            self.avg_daily_return = np.mean(self.recent_returns)
            self.return_std = np.std(self.recent_returns)
        else:
            self.avg_daily_return = 0
            self.return_std = 0.01
        
        logger.info(f"Model trained with {len(selected_feature_cols)} features (selected from {len(all_feature_cols)}), {len(X_train)} samples")
        logger.info(f"Training stats - Last NAV: {self.last_train_nav:.2f}, Avg daily return: {self.avg_daily_return*100:.4f}%")
        return True
    
    def predict_single(self, X_scaled, prev_nav=None, day_ahead=1):
        """Make prediction using weighted ensemble with trend adjustment"""
        xgb_pred = self.model.predict(X_scaled)[0]
        
        if self.use_ensemble and self.ensemble_models:
            gb_pred = self.ensemble_models['gb'].predict(X_scaled)[0]
            rf_pred = self.ensemble_models['rf'].predict(X_scaled)[0]
            
            # Weighted average - XGBoost typically performs best
            ensemble_pred = (xgb_pred * 0.5) + (gb_pred * 0.3) + (rf_pred * 0.2)
        else:
            ensemble_pred = xgb_pred
        
        # Apply trend-following adjustment if we have previous NAV
        if prev_nav is not None and hasattr(self, 'avg_daily_return'):
            # Calculate expected value based on recent trend
            trend_pred = prev_nav * (1 + self.avg_daily_return * day_ahead)
            
            # Blend ensemble prediction with trend prediction
            # Give more weight to trend for short-term predictions
            trend_weight = max(0.3, 0.5 - (day_ahead * 0.05))  # Decrease trend weight for longer horizons
            adjusted_pred = (ensemble_pred * (1 - trend_weight)) + (trend_pred * trend_weight)
            
            # Ensure prediction doesn't deviate too much from previous NAV (max 5% per day)
            max_change = prev_nav * 0.05 * day_ahead
            if adjusted_pred > prev_nav + max_change:
                adjusted_pred = prev_nav + max_change
            elif adjusted_pred < prev_nav - max_change:
                adjusted_pred = prev_nav - max_change
            
            return adjusted_pred
        
        return ensemble_pred
    
    def predict_with_momentum(self, df_pred, current_nav):
        """Calculate momentum-based adjustment factor"""
        if len(df_pred) < 5:
            return 1.0
        
        recent_navs = df_pred['nav'].tail(5).values
        returns = np.diff(recent_navs) / recent_navs[:-1]
        
        # Momentum factor: if recent trend is up, adjust upward slightly
        momentum = np.mean(returns)
        
        # Limit momentum adjustment to ±2%
        momentum_factor = 1 + np.clip(momentum * 0.5, -0.02, 0.02)
        
        return momentum_factor
    
    def predict_next_days(self, df, days=3):
        """Predict NAV for next n days using ensemble with trend adjustment"""
        if self.model is None:
            return None
        
        predictions = []
        df_pred = df.copy()
        last_date = df_pred['date'].max()
        prev_nav = float(df_pred['nav'].iloc[-1])
        
        for i in range(days):
            # Create features for prediction
            df_features = self.create_features(df_pred)
            last_row = df_features.iloc[-1:][self.feature_cols]
            
            # Handle NaN and infinite values
            last_row_values = np.nan_to_num(last_row.values, nan=0.0, posinf=0.0, neginf=0.0)
            
            # Scale and predict with trend adjustment
            X_scaled = self.scaler.transform(last_row_values)
            pred_nav = self.predict_single(X_scaled, prev_nav=prev_nav, day_ahead=1)
            
            # Apply momentum adjustment
            momentum_factor = self.predict_with_momentum(df_pred, prev_nav)
            pred_nav = pred_nav * momentum_factor
            
            # Add prediction to dataframe for next iteration
            next_date = last_date + timedelta(days=i+1)
            # Skip weekends
            while next_date.weekday() >= 5:
                next_date += timedelta(days=1)
            
            new_row = pd.DataFrame({
                'date': [next_date],
                'nav': [pred_nav]
            })
            df_pred = pd.concat([df_pred, new_row], ignore_index=True)
            prev_nav = pred_nav  # Update for next iteration
            
            predictions.append({
                'date': next_date.strftime('%Y-%m-%d'),
                'day': i + 1,
                'predicted_nav': round(float(pred_nav), 2)
            })
        
        return predictions
    
    def predict_date_range(self, df, start_date, end_date):
        """Predict NAV for specific date range using ensemble with trend adjustment"""
        if self.model is None:
            return None
        
        predictions = []
        df_pred = df.copy()
        prev_nav = float(df_pred['nav'].iloc[-1])
        
        # Convert string dates to datetime
        if isinstance(start_date, str):
            start_date = pd.to_datetime(start_date)
        if isinstance(end_date, str):
            end_date = pd.to_datetime(end_date)
        
        # Get the last date in training data
        last_training_date = df_pred['date'].max()
        
        # We need to predict day by day from last_training_date to end_date
        current_date = last_training_date + timedelta(days=1)
        
        while current_date <= end_date:
            # Skip weekends
            if current_date.weekday() >= 5:
                current_date += timedelta(days=1)
                continue
            
            # Create features for prediction
            df_features = self.create_features(df_pred)
            last_row = df_features.iloc[-1:][self.feature_cols]
            
            # Handle NaN and infinite values
            last_row_values = np.nan_to_num(last_row.values, nan=0.0, posinf=0.0, neginf=0.0)
            
            # Scale and predict with trend adjustment
            X_scaled = self.scaler.transform(last_row_values)
            pred_nav = self.predict_single(X_scaled, prev_nav=prev_nav, day_ahead=1)
            
            # Apply momentum adjustment
            momentum_factor = self.predict_with_momentum(df_pred, prev_nav)
            pred_nav = pred_nav * momentum_factor
            
            # Add prediction to dataframe for next iteration
            new_row = pd.DataFrame({
                'date': [current_date],
                'nav': [pred_nav]
            })
            df_pred = pd.concat([df_pred, new_row], ignore_index=True)
            prev_nav = pred_nav
            
            # Only include in results if within requested range
            if current_date >= start_date:
                predictions.append({
                    'date': current_date.strftime('%Y-%m-%d'),
                    'predicted_nav': round(float(pred_nav), 2)
                })
            
            current_date += timedelta(days=1)
        
        return predictions
    
    def predict_with_backtest(self, df, start_date, end_date):
        """
        Predict and compare with actual values (backtest) using ensemble with trend adjustment
        Used when the date range has actual data available
        """
        # Convert string dates to datetime
        if isinstance(start_date, str):
            start_date = pd.to_datetime(start_date)
        if isinstance(end_date, str):
            end_date = pd.to_datetime(end_date)
        
        # Filter training data to only use data before start_date
        df_train = df[df['date'] < start_date].copy()
        
        logger.info(f"Backtest: Training data has {len(df_train)} records before {start_date}")
        
        if len(df_train) < 50:
            logger.error(f"Not enough training data before start_date: {len(df_train)} < 50")
            return None
        
        # Retrain model with data before start_date
        if not self.train_model(df_train):
            logger.error("Failed to train model for backtest")
            return None
        
        # Get actual data for the date range
        df_actual = df[(df['date'] >= start_date) & (df['date'] <= end_date)].copy()
        
        predictions = []
        df_pred = df_train.copy()
        prev_nav = float(df_pred['nav'].iloc[-1])
        
        current_date = start_date
        
        while current_date <= end_date:
            # Skip weekends
            if current_date.weekday() >= 5:
                current_date += timedelta(days=1)
                continue
            
            # Create features for prediction
            df_features = self.create_features(df_pred)
            last_row = df_features.iloc[-1:][self.feature_cols]
            
            # Handle NaN and infinite values
            last_row_values = np.nan_to_num(last_row.values, nan=0.0, posinf=0.0, neginf=0.0)
            
            # Scale and predict using ensemble with trend adjustment
            X_scaled = self.scaler.transform(last_row_values)
            pred_nav = self.predict_single(X_scaled, prev_nav=prev_nav, day_ahead=1)
            
            # Apply momentum adjustment
            momentum_factor = self.predict_with_momentum(df_pred, prev_nav)
            pred_nav = pred_nav * momentum_factor
            
            # Get actual value if available
            actual_row = df_actual[df_actual['date'] == current_date]
            actual_nav = float(actual_row['nav'].values[0]) if len(actual_row) > 0 else None
            
            # Calculate error if actual value exists
            error = None
            error_percent = None
            if actual_nav is not None:
                error = round(float(pred_nav) - actual_nav, 2)
                error_percent = round((error / actual_nav) * 100, 4)
            
            prediction_item = {
                'date': current_date.strftime('%Y-%m-%d'),
                'predicted_nav': round(float(pred_nav), 2),
                'actual_nav': round(actual_nav, 2) if actual_nav else None,
                'error': error,
                'error_percent': error_percent
            }
            predictions.append(prediction_item)
            
            # Add actual value to dataframe for next iteration (use actual for better next prediction)
            nav_to_add = actual_nav if actual_nav else pred_nav
            new_row = pd.DataFrame({
                'date': [current_date],
                'nav': [nav_to_add]
            })
            df_pred = pd.concat([df_pred, new_row], ignore_index=True)
            prev_nav = nav_to_add  # Update prev_nav for next prediction
            
            current_date += timedelta(days=1)
        
        return predictions

predictor = NAVPredictor()

@app.route('/health', methods=['GET'])
def health():
    return jsonify({'status': 'healthy'})

@app.route('/predict', methods=['POST'])
def predict():
    """
    Endpoint untuk prediksi NAV reksadana
    
    Request body:
    {
        "pid": 123,           // Product ID dari Bareksa
        "days": 3,            // Jumlah hari prediksi (default: 3)
        "history_days": 365   // Jumlah hari data historis (default: 365)
    }
    """
    try:
        data = request.get_json()
        
        if not data or 'pid' not in data:
            return jsonify({
                'error': 'Missing required field: pid',
                'success': False
            }), 400
        
        pid = data['pid']
        prediction_days = data.get('days', 3)
        history_days = data.get('history_days', 365)
        
        # Limit prediction days
        if prediction_days > 7:
            prediction_days = 7
        
        logger.info(f"Fetching NAV data for PID: {pid}")
        
        # Fetch historical data
        nav_data = predictor.fetch_nav_data(pid, history_days)
        
        if not nav_data:
            return jsonify({
                'error': 'Failed to fetch NAV data from Bareksa',
                'success': False
            }), 500
        
        # Prepare data
        df = predictor.prepare_data(nav_data)
        
        if df is None or len(df) < 50:
            return jsonify({
                'error': 'Insufficient historical data for prediction',
                'success': False,
                'data_points': len(df) if df is not None else 0
            }), 400
        
        # Train model
        if not predictor.train_model(df):
            return jsonify({
                'error': 'Failed to train prediction model',
                'success': False
            }), 500
        
        # Make predictions
        predictions = predictor.predict_next_days(df, prediction_days)
        
        if not predictions:
            return jsonify({
                'error': 'Failed to generate predictions',
                'success': False
            }), 500
        
        # Get latest actual NAV
        latest_nav = float(df.iloc[-1]['nav'])
        latest_date = df.iloc[-1]['date'].strftime('%Y-%m-%d')
        
        # Calculate prediction statistics
        avg_prediction = np.mean([p['predicted_nav'] for p in predictions])
        trend = 'up' if predictions[-1]['predicted_nav'] > latest_nav else 'down'
        change_percent = ((predictions[-1]['predicted_nav'] - latest_nav) / latest_nav) * 100
        
        return jsonify({
            'success': True,
            'pid': pid,
            'latest_nav': {
                'date': latest_date,
                'value': round(latest_nav, 2)
            },
            'predictions': predictions,
            'summary': {
                'average_predicted_nav': round(avg_prediction, 2),
                'trend': trend,
                'change_percent': round(change_percent, 4),
                'prediction_days': prediction_days,
                'data_points_used': len(df)
            }
        })
        
    except Exception as e:
        logger.error(f"Prediction error: {e}")
        return jsonify({
            'error': str(e),
            'success': False
        }), 500

@app.route('/predict/custom', methods=['POST'])
def predict_custom():
    """
    Endpoint untuk prediksi dengan data custom (tanpa fetch dari Bareksa)
    
    Request body:
    {
        "nav_data": [
            {"date": "2024-01-01", "nav": 1234.56},
            ...
        ],
        "days": 3
    }
    """
    try:
        data = request.get_json()
        
        if not data or 'nav_data' not in data:
            return jsonify({
                'error': 'Missing required field: nav_data',
                'success': False
            }), 400
        
        nav_data = {'data': data['nav_data']}
        prediction_days = data.get('days', 3)
        
        if prediction_days > 7:
            prediction_days = 7
        
        # Prepare data
        df = predictor.prepare_data(nav_data)
        
        if df is None or len(df) < 50:
            return jsonify({
                'error': 'Insufficient historical data for prediction (minimum 50 data points)',
                'success': False
            }), 400
        
        # Train and predict
        if not predictor.train_model(df):
            return jsonify({
                'error': 'Failed to train prediction model',
                'success': False
            }), 500
        
        predictions = predictor.predict_next_days(df, prediction_days)
        
        if not predictions:
            return jsonify({
                'error': 'Failed to generate predictions',
                'success': False
            }), 500
        
        latest_nav = float(df.iloc[-1]['nav'])
        latest_date = df.iloc[-1]['date'].strftime('%Y-%m-%d')
        
        return jsonify({
            'success': True,
            'latest_nav': {
                'date': latest_date,
                'value': round(latest_nav, 2)
            },
            'predictions': predictions,
            'data_points_used': len(df)
        })
        
    except Exception as e:
        logger.error(f"Custom prediction error: {e}")
        return jsonify({
            'error': str(e),
            'success': False
        }), 500


@app.route('/predict/range', methods=['POST'])
def predict_range():
    """
    Endpoint untuk prediksi NAV pada range tanggal tertentu
    
    Request body:
    {
        "pid": 123,              // Product ID dari Bareksa
        "start_date": "2025-11-24",  // Tanggal mulai prediksi
        "end_date": "2025-11-27",    // Tanggal akhir prediksi
        "backtest": true         // Jika true, akan membandingkan dengan data aktual (optional)
    }
    
    Jika backtest=true dan tanggal sudah lewat, akan menampilkan perbandingan prediksi vs aktual
    """
    try:
        data = request.get_json()
        
        if not data or 'pid' not in data:
            return jsonify({
                'error': 'Missing required field: pid',
                'success': False
            }), 400
        
        if 'start_date' not in data or 'end_date' not in data:
            return jsonify({
                'error': 'Missing required fields: start_date and end_date',
                'success': False
            }), 400
        
        pid = data['pid']
        start_date = data['start_date']
        end_date = data['end_date']
        backtest = data.get('backtest', False)
        
        # Validate dates
        try:
            start_dt = pd.to_datetime(start_date)
            end_dt = pd.to_datetime(end_date)
        except:
            return jsonify({
                'error': 'Invalid date format. Use YYYY-MM-DD',
                'success': False
            }), 400
        
        if start_dt > end_dt:
            return jsonify({
                'error': 'start_date must be before or equal to end_date',
                'success': False
            }), 400
        
        # Limit to 30 days max
        if (end_dt - start_dt).days > 30:
            return jsonify({
                'error': 'Maximum prediction range is 30 days',
                'success': False
            }), 400
        
        logger.info(f"Fetching NAV data for PID: {pid}, range: {start_date} to {end_date}")
        
        # Fetch historical data
        nav_data = predictor.fetch_nav_data(pid, 730)  # Get 2 years of data
        
        if not nav_data:
            return jsonify({
                'error': 'Failed to fetch NAV data from Bareksa',
                'success': False
            }), 500
        
        # Prepare data
        df = predictor.prepare_data(nav_data)
        
        if df is None or len(df) < 50:
            return jsonify({
                'error': 'Insufficient historical data for prediction',
                'success': False,
                'data_points': len(df) if df is not None else 0
            }), 400
        
        # Check if we should do backtest (dates are in the past and data exists)
        last_data_date = df['date'].max()
        
        if backtest and end_dt <= last_data_date:
            # Backtest mode - compare predictions with actual values
            predictions = predictor.predict_with_backtest(df, start_date, end_date)
            
            if not predictions:
                return jsonify({
                    'error': 'Failed to generate backtest predictions',
                    'success': False
                }), 500
            
            # Calculate backtest statistics
            valid_predictions = [p for p in predictions if p['actual_nav'] is not None]
            if valid_predictions:
                mae = np.mean([abs(p['error']) for p in valid_predictions])
                mape = np.mean([abs(p['error_percent']) for p in valid_predictions])
            else:
                mae = None
                mape = None
            
            return jsonify({
                'success': True,
                'pid': pid,
                'mode': 'backtest',
                'date_range': {
                    'start': start_date,
                    'end': end_date
                },
                'predictions': predictions,
                'backtest_metrics': {
                    'mean_absolute_error': round(mae, 2) if mae else None,
                    'mean_absolute_percentage_error': round(mape, 4) if mape else None,
                    'data_points': len(valid_predictions)
                }
            })
        else:
            # Future prediction mode
            # Train model with all available data
            if not predictor.train_model(df):
                return jsonify({
                    'error': 'Failed to train prediction model',
                    'success': False
                }), 500
            
            predictions = predictor.predict_date_range(df, start_date, end_date)
            
            if not predictions:
                return jsonify({
                    'error': 'Failed to generate predictions',
                    'success': False
                }), 500
            
            # Get latest actual NAV
            latest_nav = float(df.iloc[-1]['nav'])
            latest_date = df.iloc[-1]['date'].strftime('%Y-%m-%d')
            
            # Calculate statistics
            avg_prediction = np.mean([p['predicted_nav'] for p in predictions])
            first_pred = predictions[0]['predicted_nav']
            last_pred = predictions[-1]['predicted_nav']
            trend = 'up' if last_pred > first_pred else 'down' if last_pred < first_pred else 'stable'
            change_percent = ((last_pred - first_pred) / first_pred) * 100 if first_pred else 0
            
            return jsonify({
                'success': True,
                'pid': pid,
                'mode': 'forecast',
                'latest_nav': {
                    'date': latest_date,
                    'value': round(latest_nav, 2)
                },
                'date_range': {
                    'start': start_date,
                    'end': end_date
                },
                'predictions': predictions,
                'summary': {
                    'average_predicted_nav': round(avg_prediction, 2),
                    'trend': trend,
                    'change_percent': round(change_percent, 4),
                    'prediction_days': len(predictions),
                    'data_points_used': len(df)
                }
            })
        
    except Exception as e:
        logger.error(f"Range prediction error: {e}")
        import traceback
        traceback.print_exc()
        return jsonify({
            'error': str(e),
            'success': False
        }), 500

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5001, debug=True)
