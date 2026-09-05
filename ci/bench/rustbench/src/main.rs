use std::env;
use std::fs;
use std::hint::black_box;
use std::path::Path;
use std::time::Instant;
use tabnas::Tabnas;

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() != 4 {
        eprintln!("usage: tabnas-rustbench <fixture> <iterations> <warmup>");
        std::process::exit(2);
    }

    let fixture = &args[1];
    let iterations: usize = args[2].parse().expect("iterations must be an integer");
    let warmup: usize = args[3].parse().expect("warmup must be an integer");
    let source = fs::read_to_string(fixture).expect("fixture must be readable UTF-8");
    let parser = Tabnas::make_json();

    for _ in 0..warmup {
        black_box(
            parser
                .parse(black_box(&source))
                .expect("warmup parse must succeed"),
        );
    }

    let mut samples = Vec::with_capacity(iterations);
    for _ in 0..iterations {
        let start = Instant::now();
        black_box(
            parser
                .parse(black_box(&source))
                .expect("measured parse must succeed"),
        );
        samples.push(start.elapsed().as_secs_f64() * 1_000.0);
    }
    samples.sort_by(f64::total_cmp);
    let median_ms = if iterations % 2 == 0 {
        (samples[iterations / 2 - 1] + samples[iterations / 2]) / 2.0
    } else {
        samples[iterations / 2]
    };
    let mib = source.len() as f64 / (1024.0 * 1024.0);
    let name = Path::new(fixture)
        .file_name()
        .and_then(|name| name.to_str())
        .unwrap_or(fixture);

    println!(
        "{{\"runtime\":\"rust\",\"parser\":\"json\",\"fixture\":\"{name}\",\"bytes\":{},\"iterations\":{iterations},\"median_ms\":{median_ms:.3},\"mib_per_s\":{:.3}}}",
        source.len(),
        mib / (median_ms / 1_000.0),
    );
}
