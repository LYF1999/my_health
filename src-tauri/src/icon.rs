use tauri::image::Image;

pub fn water_drop<'a>() -> Image<'a> {
    const W: u32 = 44;
    const H: u32 = 44;
    let mut rgba = vec![0u8; (W * H * 4) as usize];

    let cx = 22.0;
    let cy = 28.0; // 下半圆圆心
    let r = 12.0; // 圆半径
    let h = 19.0; // 上半泪滴高度
    let top_y = cy - h;

    // 是否在泪滴内部
    let inside = |sx: f64, sy: f64| -> bool {
        let dx = sx - cx;
        if sy >= cy {
            dx * dx + (sy - cy).powi(2) <= r * r
        } else if sy >= top_y {
            let t = (cy - sy) / h;
            // 平滑收尖到顶端：half_width = r * (1 - t²)
            let half = r * (1.0 - t * t);
            dx.abs() <= half
        } else {
            false
        }
    };

    // 4x4 超采样抗锯齿
    for y in 0..H {
        for x in 0..W {
            let mut hits = 0u32;
            for sy in 0..4 {
                for sx in 0..4 {
                    let fx = x as f64 + (sx as f64 + 0.5) / 4.0;
                    let fy = y as f64 + (sy as f64 + 0.5) / 4.0;
                    if inside(fx, fy) {
                        hits += 1;
                    }
                }
            }
            if hits > 0 {
                let alpha = (hits as f64 / 16.0 * 255.0).round() as u8;
                let off = ((y * W + x) * 4) as usize;
                // 顶部稍亮、底部稍深的简易"高光"渐变
                let depth = ((y as f64 - top_y) / (cy + r - top_y)).clamp(0.0, 1.0);
                let r_ch = (90.0 - 35.0 * depth).round() as u8; // 90→55
                let g_ch = (155.0 - 30.0 * depth).round() as u8; // 155→125
                let b_ch = (255.0 - 25.0 * depth).round() as u8; // 255→230
                rgba[off] = r_ch;
                rgba[off + 1] = g_ch;
                rgba[off + 2] = b_ch;
                rgba[off + 3] = alpha;
            }
        }
    }

    Image::new_owned(rgba, W, H)
}
