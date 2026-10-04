function advance(x,y,z,vx,vy,vz,mass,dt) {
    for(let i=0;i<5;i++)for(let j=i+1;j<5;j++) {
        const dx=x[i]-x[j],dy=y[i]-y[j],dz=z[i]-z[j];
        const d2=dx*dx+dy*dy+dz*dz,mag=dt/(d2*Math.sqrt(d2));
        vx[i]-=dx*mass[j]*mag;
        vy[i]-=dy*mass[j]*mag;
        vz[i]-=dz*mass[j]*mag;
        vx[j]+=dx*mass[i]*mag;
        vy[j]+=dy*mass[i]*mag;
        vz[j]+=dz*mass[i]*mag;
    }
    for(let i=0;i<5;i++) {
        x[i]+=dt*vx[i];
        y[i]+=dt*vy[i];
        z[i]+=dt*vz[i];
    }
}
function energy(x,y,z,vx,vy,vz,mass) {
    let e=0;
    for(let i=0;i<5;i++) {
        e+=0.5*mass[i]*(vx[i]*vx[i]+vy[i]*vy[i]+vz[i]*vz[i]);
        for(let j=i+1;j<5;j++) {
            const dx=x[i]-x[j],dy=y[i]-y[j],dz=z[i]-z[j];
            e-=mass[i]*mass[j]/Math.sqrt(dx*dx+dy*dy+dz*dz);
        }
    }
    return e;
}
function run() {
    const pi=Math.PI,sm=4*pi*pi,dy=365.24;
    const x=[
        0,
        4.841431442464721,
        8.34336671824458,
        12.943505513317835,
        15.379697114850945,
    ];
    const y=[
        0,
        -1.1603200440274284,
        4.124798564124305,
        -15.111514016986319,
        -25.919314609987964,
    ];
    const z=[
        0,
        -0.10362204447112311,
        -0.4035234171143214,
        -0.2237057963357768,
        0.17925877295037118,
    ];
    const vx=[
        0,
        .00166007664274403*dy,
        .00283009096225471*dy,
        .00296460137564761*dy,
        .00268067772490389*dy,
    ];
    const vy=[
        0,
        .0076990111841974*dy,
        .00453000209594919*dy,
        .0023784717395948*dy,
        .00162824170038242*dy,
    ];
    const vz=[
        0,
        -.00006902509938426*dy,
        -.00019131288713706*dy,
        -.0002958928886558*dy,
        -.00095159225451337*dy,
    ];
    const mass=[
        sm,
        .0009547919384243266*sm,
        .0002858859806661308*sm,
        .00004366244043351563*sm,
        .00005151389020466115*sm,
    ];
    let px=0,py=0,pz=0;
    for(let i=0;i<5;i++) {
        px+=vx[i]*mass[i];
        py+=vy[i]*mass[i];
        pz+=vz[i]*mass[i]
    }
    vx[0]=-px/sm;
    vy[0]=-py/sm;
    vz[0]=-pz/sm;
    for(let i=0;i<100;i++)advance(x,y,z,vx,vy,vz,mass,.01);
    return Math.trunc(energy(x,y,z,vx,vy,vz,mass)*1e9);
}
