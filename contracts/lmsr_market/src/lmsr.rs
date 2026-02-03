//! LMSR (Logarithmic Market Scoring Rule) pricing implementation.
//!
//! All calculations use fixed-point arithmetic with SCALE_FACTOR (10^7).
//! This matches Stellar's 7 decimal place precision.
//!
//! Formulas:
//! - Cost function: C(q) = b * ln(e^(qYes/b) + e^(qNo/b))
//! - Price: P(yes) = e^(qYes/b) / (e^(qYes/b) + e^(qNo/b))
//! - Buy cost: C(q_new) - C(q_old)

use crate::error::MarketError;
use crate::storage::SCALE_FACTOR;

/// Maximum iterations for exp approximation
const EXP_ITERATIONS: u32 = 20;

/// Scaled exp function using Taylor series: e^x = 1 + x + x²/2! + x³/3! + ...
/// Input and output are scaled by SCALE_FACTOR.
/// For numerical stability, we limit the input range.
fn exp_scaled(x: i128) -> Result<i128, MarketError> {
    // For very negative x, return near-zero
    if x < -20 * SCALE_FACTOR {
        return Ok(0);
    }
    // For very large x, cap to prevent overflow
    if x > 20 * SCALE_FACTOR {
        return Err(MarketError::Overflow);
    }

    // Taylor series: e^x = sum(x^n / n!) for n = 0 to infinity
    let mut result: i128 = SCALE_FACTOR; // 1.0 scaled
    let mut term: i128 = SCALE_FACTOR; // Current term (x^n / n!)

    for n in 1..=EXP_ITERATIONS {
        // term = term * x / (n * SCALE_FACTOR)
        term = term.checked_mul(x).ok_or(MarketError::Overflow)?;
        term = term.checked_div(n as i128 * SCALE_FACTOR).ok_or(MarketError::Overflow)?;

        result = result.checked_add(term).ok_or(MarketError::Overflow)?;

        // Early termination if term becomes negligible
        if term.abs() < 1 {
            break;
        }
    }

    Ok(result.max(0))
}

/// Natural logarithm using Newton-Raphson method.
/// Input and output are scaled by SCALE_FACTOR.
/// ln(x) where x is scaled by SCALE_FACTOR.
fn ln_scaled(x: i128) -> Result<i128, MarketError> {
    if x <= 0 {
        return Err(MarketError::Overflow);
    }

    // For x = SCALE_FACTOR (i.e., 1.0), ln(1) = 0
    if x == SCALE_FACTOR {
        return Ok(0);
    }

    // Use the identity: ln(x) = 2 * atanh((x-1)/(x+1))
    // atanh(y) = y + y³/3 + y⁵/5 + ...
    //
    // For better convergence, normalize x to [1, 2) range:
    // ln(x * 2^n) = ln(x) + n * ln(2)

    let mut normalized = x;
    let mut n: i128 = 0;

    // Scale down to [SCALE_FACTOR, 2*SCALE_FACTOR)
    while normalized >= 2 * SCALE_FACTOR {
        normalized = normalized.checked_div(2).ok_or(MarketError::Overflow)?;
        n += 1;
    }

    // Scale up if less than 1
    while normalized < SCALE_FACTOR && normalized > 0 {
        normalized = normalized.checked_mul(2).ok_or(MarketError::Overflow)?;
        n -= 1;
    }

    // Now normalized is in [SCALE_FACTOR, 2*SCALE_FACTOR)
    // Compute ln(normalized) using series for ln(1+y) where y = (normalized - SCALE_FACTOR) / SCALE_FACTOR

    // y = (normalized - SCALE_FACTOR) scaled
    let y_num = normalized - SCALE_FACTOR;

    // ln(1+y) = y - y²/2 + y³/3 - y⁴/4 + ...
    let mut result: i128 = 0;
    let mut y_power = y_num; // y^1 * SCALE_FACTOR
    let mut sign: i128 = 1;

    for k in 1..=30 {
        let term = sign * y_power / k;
        result = result.checked_add(term).ok_or(MarketError::Overflow)?;

        // y_power = y_power * y / SCALE_FACTOR
        y_power = y_power
            .checked_mul(y_num)
            .ok_or(MarketError::Overflow)?
            .checked_div(SCALE_FACTOR)
            .ok_or(MarketError::Overflow)?;

        sign = -sign;

        if y_power.abs() < 1 {
            break;
        }
    }

    // Add n * ln(2)
    let ln2_scaled: i128 = 6_931_472; // ln(2) * 10^7
    let adjustment = n.checked_mul(ln2_scaled).ok_or(MarketError::Overflow)?;
    result = result.checked_add(adjustment).ok_or(MarketError::Overflow)?;

    Ok(result)
}

/// Calculate the LMSR cost function: C(q) = b * ln(e^(qYes/b) + e^(qNo/b))
/// All inputs are scaled by SCALE_FACTOR.
pub fn cost(q_yes: i128, q_no: i128, b: i128) -> Result<i128, MarketError> {
    if b <= 0 {
        return Err(MarketError::InvalidLiquidity);
    }

    // Calculate qYes/b and qNo/b (result still scaled)
    let q_yes_over_b = q_yes.checked_mul(SCALE_FACTOR).ok_or(MarketError::Overflow)?
        .checked_div(b).ok_or(MarketError::Overflow)?;
    let q_no_over_b = q_no.checked_mul(SCALE_FACTOR).ok_or(MarketError::Overflow)?
        .checked_div(b).ok_or(MarketError::Overflow)?;

    // Use log-sum-exp trick for numerical stability:
    // ln(e^a + e^b) = max(a,b) + ln(1 + e^(min-max))
    let max_q = q_yes_over_b.max(q_no_over_b);
    let min_q = q_yes_over_b.min(q_no_over_b);

    let diff = min_q.checked_sub(max_q).ok_or(MarketError::Overflow)?;
    let exp_diff = exp_scaled(diff)?;
    let sum = SCALE_FACTOR.checked_add(exp_diff).ok_or(MarketError::Overflow)?;
    let ln_sum = ln_scaled(sum)?;

    let inside = max_q.checked_add(ln_sum).ok_or(MarketError::Overflow)?;

    // C = b * inside / SCALE_FACTOR (to maintain proper scaling)
    let result = b.checked_mul(inside).ok_or(MarketError::Overflow)?
        .checked_div(SCALE_FACTOR).ok_or(MarketError::Overflow)?;

    Ok(result)
}

/// Calculate the cost to buy `amount` of `outcome` tokens.
/// Returns the cost in collateral (scaled by SCALE_FACTOR).
pub fn calculate_buy_cost(
    q_yes: i128,
    q_no: i128,
    amount: i128,
    outcome: u32,
    b: i128,
) -> Result<i128, MarketError> {
    if amount <= 0 {
        return Err(MarketError::InvalidAmount);
    }

    let cost_before = cost(q_yes, q_no, b)?;

    let cost_after = match outcome {
        0 => cost(q_yes.checked_add(amount).ok_or(MarketError::Overflow)?, q_no, b)?,
        1 => cost(q_yes, q_no.checked_add(amount).ok_or(MarketError::Overflow)?, b)?,
        _ => return Err(MarketError::InvalidOutcome),
    };

    cost_after.checked_sub(cost_before).ok_or(MarketError::Overflow)
}

/// Calculate the return from selling `amount` of `outcome` tokens.
/// Returns the collateral received (scaled by SCALE_FACTOR).
pub fn calculate_sell_return(
    q_yes: i128,
    q_no: i128,
    amount: i128,
    outcome: u32,
    b: i128,
) -> Result<i128, MarketError> {
    if amount <= 0 {
        return Err(MarketError::InvalidAmount);
    }

    let cost_before = cost(q_yes, q_no, b)?;

    let cost_after = match outcome {
        0 => {
            if q_yes < amount {
                return Err(MarketError::InsufficientBalance);
            }
            cost(q_yes.checked_sub(amount).ok_or(MarketError::Overflow)?, q_no, b)?
        }
        1 => {
            if q_no < amount {
                return Err(MarketError::InsufficientBalance);
            }
            cost(q_yes, q_no.checked_sub(amount).ok_or(MarketError::Overflow)?, b)?
        }
        _ => return Err(MarketError::InvalidOutcome),
    };

    cost_before.checked_sub(cost_after).ok_or(MarketError::Overflow)
}

/// Calculate the current price (probability) of an outcome.
/// Returns price scaled by SCALE_FACTOR (0 to SCALE_FACTOR represents 0 to 1).
pub fn calculate_price(q_yes: i128, q_no: i128, outcome: u32, b: i128) -> Result<i128, MarketError> {
    if b <= 0 {
        return Err(MarketError::InvalidLiquidity);
    }

    // P(yes) = e^(qYes/b) / (e^(qYes/b) + e^(qNo/b))
    let q_yes_over_b = q_yes.checked_mul(SCALE_FACTOR).ok_or(MarketError::Overflow)?
        .checked_div(b).ok_or(MarketError::Overflow)?;
    let q_no_over_b = q_no.checked_mul(SCALE_FACTOR).ok_or(MarketError::Overflow)?
        .checked_div(b).ok_or(MarketError::Overflow)?;

    let exp_yes = exp_scaled(q_yes_over_b)?;
    let exp_no = exp_scaled(q_no_over_b)?;
    let sum = exp_yes.checked_add(exp_no).ok_or(MarketError::Overflow)?;

    if sum == 0 {
        return Err(MarketError::Overflow);
    }

    match outcome {
        0 => Ok(exp_yes.checked_mul(SCALE_FACTOR).ok_or(MarketError::Overflow)?
            .checked_div(sum).ok_or(MarketError::Overflow)?),
        1 => Ok(exp_no.checked_mul(SCALE_FACTOR).ok_or(MarketError::Overflow)?
            .checked_div(sum).ok_or(MarketError::Overflow)?),
        _ => Err(MarketError::InvalidOutcome),
    }
}

/// Calculate initial liquidity required: b * ln(2)
pub fn initial_liquidity(b: i128) -> Result<i128, MarketError> {
    if b <= 0 {
        return Err(MarketError::InvalidLiquidity);
    }
    let ln2_scaled: i128 = 6_931_472; // ln(2) * 10^7
    b.checked_mul(ln2_scaled).ok_or(MarketError::Overflow)?
        .checked_div(SCALE_FACTOR).ok_or(MarketError::Overflow)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_exp_scaled() {
        // e^0 = 1
        assert_eq!(exp_scaled(0).unwrap(), SCALE_FACTOR);

        // e^1 ≈ 2.718
        let e1 = exp_scaled(SCALE_FACTOR).unwrap();
        assert!(e1 > 27_000_000 && e1 < 28_000_000, "e^1 = {}", e1);
    }

    #[test]
    fn test_ln_scaled() {
        // ln(1) = 0
        assert_eq!(ln_scaled(SCALE_FACTOR).unwrap(), 0);

        // ln(e) ≈ 1
        let ln_e = ln_scaled(27_182_818).unwrap();
        assert!(ln_e > 9_900_000 && ln_e < 10_100_000, "ln(e) = {}", ln_e);
    }

    #[test]
    fn test_price_at_equilibrium() {
        let b = 100 * SCALE_FACTOR;
        // When qYes = qNo, price should be 0.5
        let price_yes = calculate_price(0, 0, 0, b).unwrap();
        let price_no = calculate_price(0, 0, 1, b).unwrap();

        assert!(price_yes > 4_900_000 && price_yes < 5_100_000, "price_yes = {}", price_yes);
        assert!(price_no > 4_900_000 && price_no < 5_100_000, "price_no = {}", price_no);
    }

    #[test]
    fn test_buy_cost_positive() {
        let b = 100 * SCALE_FACTOR;
        let cost = calculate_buy_cost(0, 0, 10 * SCALE_FACTOR, 0, b).unwrap();
        assert!(cost > 0, "Buy cost should be positive");
    }

    #[test]
    fn test_initial_liquidity() {
        let b = 100 * SCALE_FACTOR;
        let liquidity = initial_liquidity(b).unwrap();
        // Should be approximately 100 * 0.693 = 69.3
        assert!(liquidity > 69 * SCALE_FACTOR && liquidity < 70 * SCALE_FACTOR,
            "initial_liquidity = {}", liquidity);
    }
}
